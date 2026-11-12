// VideoEncoder - interfață grafică pentru FFmpeg (locală + externă)
// https://github.com/maxvideostar/VideoEncoder
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const AppName = "VideoEncoder"
const AppVersion = "1.5.5"

//go:embed web
var webFS embed.FS

type Config struct {
	Port        int    `json:"port"`
	AMFHighBF   bool   `json:"amf_high_bf"` // experimental: AMD High + B-frames (oprit implicit)
	Bind        string `json:"bind"`
	Username    string `json:"username"`
	PassHash    string `json:"pass_hash"`
	Salt        string `json:"salt"`
	LocalNoAuth bool   `json:"local_no_auth"`
	FFmpegPath  string `json:"ffmpeg_path"`
	FFprobePath string `json:"ffprobe_path"`
	OutputDir   string `json:"output_dir"`
	MaxJobs     int    `json:"max_jobs"`
	LowPriority bool   `json:"low_priority"`
	OpenWindow  bool   `json:"open_window"`
	DefaultPass bool   `json:"default_pass"`
	QSVModeOK   string `json:"qsv_mode_ok"` // mod QSV detectat ca funcțional
	McastIface  string `json:"mcast_iface"` // IP-ul plăcii de rețea pe care vine multicastul (gol = automat)
}

var (
	baseDir string
	cfg     Config
	cfgMu   sync.RWMutex
	jobs    *JobManager
)

func cfgPath() string { return filepath.Join(baseDir, "config.json") }

func loadConfig() {
	cfg = Config{
		Port: 8090, Bind: "0.0.0.0", Username: "admin", LocalNoAuth: true,
		OutputDir: filepath.Join(baseDir, "output"), MaxJobs: 1, OpenWindow: true,
	}
	b, err := os.ReadFile(cfgPath())
	if err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			log.Printf("config.json invalid: %v (folosesc valorile implicite)", err)
		}
	}
	if cfg.PassHash == "" {
		cfg.Salt = randToken(16)
		cfg.PassHash = hashPass("admin", cfg.Salt)
		cfg.DefaultPass = true
	}
	if cfg.Port <= 0 {
		cfg.Port = 8090
	}
	if cfg.MaxJobs <= 0 {
		cfg.MaxJobs = 1
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(baseDir, "output")
	}
	saveConfig()
}

func saveConfig() {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath(), b, 0600); err != nil {
		log.Printf("nu pot salva config.json: %v", err)
	}
}

func getCfg() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

func main() {
	exe, _ := os.Executable()
	baseDir = filepath.Dir(exe)
	dataDir := flag.String("data", "", "director date (implicit: lângă exe)")
	noGUI := flag.Bool("nogui", false, "nu deschide fereastra locală")
	port := flag.Int("port", 0, "port HTTP (suprascrie config)")
	fInstall := flag.Bool("install", false, "instalează și pornește serviciul Windows (necesită administrator)")
	fUninstall := flag.Bool("uninstall", false, "oprește și dezinstalează serviciul Windows")
	fStart := flag.Bool("start", false, "pornește serviciul Windows")
	fStop := flag.Bool("stop", false, "oprește serviciul Windows")
	fService := flag.Bool("service", false, "(intern) rulare ca serviciu Windows")
	svcUser := flag.String("svc-user", "", "cont pentru serviciu (implicit LocalSystem), ex: .\\Cristian")
	svcPass := flag.String("svc-pass", "", "parola contului pentru serviciu")
	fVersion := flag.Bool("version", false, "afișează versiunea")
	setUser := flag.String("set-user", "", "setează utilizatorul pentru accesul extern și iese")
	setPass := flag.String("set-password", "", "setează parola pentru accesul extern și iese")
	setPort := flag.Int("set-port", 0, "setează portul HTTP și iese")
	setFF := flag.String("set-ffmpeg", "", "setează calea ffmpeg.exe (ffprobe.exe din același folder) și iese")
	flag.Parse()
	if *fVersion {
		fmt.Println(AppVersion)
		return
	}
	if *dataDir != "" {
		baseDir = *dataDir
	}
	os.MkdirAll(baseDir, 0755)

	if *setUser != "" || *setPass != "" || *setPort > 0 || *setFF != "" {
		loadConfig()
		if *setUser != "" {
			cfg.Username = *setUser
		}
		if *setPass != "" {
			cfg.Salt = randToken(16)
			cfg.PassHash = hashPass(*setPass, cfg.Salt)
			cfg.DefaultPass = false
		}
		if *setPort > 0 && *setPort < 65536 {
			cfg.Port = *setPort
		}
		if *setFF != "" {
			cfg.FFmpegPath = *setFF
			cfg.FFprobePath = filepath.Join(filepath.Dir(*setFF), exeName("ffprobe"))
			cfg.QSVModeOK = ""
		}
		saveConfig()
		fmt.Println("config.json actualizat")
		return
	}

	if *fInstall || *fUninstall || *fStart || *fStop {
		os.Exit(serviceCLI(*fInstall, *fUninstall, *fStart, *fStop, *svcUser, *svcPass))
	}
	if *fService {
		*noGUI = true
	}

	lf, err := os.OpenFile(filepath.Join(baseDir, "videoencoder.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(io.MultiWriter(lf, os.Stdout))
	}
	log.Printf("%s %s pornește (director: %s)", AppName, AppVersion, baseDir)

	loadConfig()
	if *port > 0 {
		cfg.Port = *port
	}
	os.MkdirAll(cfg.OutputDir, 0755)
	os.MkdirAll(filepath.Join(baseDir, "uploads"), 0755)

	c := getCfg()
	localURL := fmt.Sprintf("http://127.0.0.1:%d/", c.Port)
	addr := net.JoinHostPort(c.Bind, strconv.Itoa(c.Port))

	if *fService {
		// rulăm ca serviciu: serverul în fundal, dispatcher-ul SCM blochează
		srvCh := make(chan *http.Server, 1)
		go func() {
			var ln net.Listener
			for i := 0; ; i++ {
				l, err := net.Listen("tcp", addr)
				if err == nil {
					ln = l
					break
				}
				if i%15 == 0 {
					log.Printf("serviciu: aștept portul %s (%v)", addr, err)
				}
				time.Sleep(2 * time.Second)
			}
			srvCh <- startServer(ln, c, localURL, false)
		}()
		err := runService(func() {
			if jobs != nil {
				jobs.KillAll()
			}
			select {
			case srv := <-srvCh:
				srv.Close()
			default:
			}
		})
		if err != nil {
			log.Printf("nu pot rula ca serviciu: %v", err)
			os.Exit(1)
		}
		log.Printf("serviciu oprit")
		os.Exit(0)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// probabil rulează deja (aplicație sau serviciu) – deschidem doar fereastra
		log.Printf("nu pot asculta pe %s: %v", addr, err)
		if !*noGUI {
			openWindow(localURL)
		}
		time.Sleep(2 * time.Second)
		os.Exit(1)
	}
	srv := startServer(ln, c, localURL, c.OpenWindow && !*noGUI)
	_ = srv
	select {}
}

func startServer(ln net.Listener, c Config, localURL string, open bool) *http.Server {

	jobs = NewJobManager()
	go sysMonitorLoop()
	go detectEncoders()

	mux := http.NewServeMux()
	registerRoutes(mux)
	srv := &http.Server{Handler: securityHeaders(mux), ReadHeaderTimeout: 20 * time.Second}

	log.Printf("Interfață locală:  %s", localURL)
	for _, ip := range lanIPs() {
		log.Printf("Interfață externă: http://%s:%d/  (utilizator: %s)", ip, c.Port, c.Username)
	}
	if c.DefaultPass {
		log.Printf("ATENȚIE: parola implicită este 'admin' – schimb-o din Setări!")
	}
	if open {
		go func() { time.Sleep(400 * time.Millisecond); openWindow(localURL) }()
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	return srv
}

// serviceCLI – acțiuni din linia de comandă / .bat
func serviceCLI(install, uninstall, start, stop bool, user, pass string) int {
	var err error
	switch {
	case install:
		if err = serviceInstall(user, pass); err == nil {
			err = serviceStart()
			if err == nil {
				log.Printf("Serviciul %s a fost instalat și pornit (pornire automată cu Windows).", ServiceName)
			}
		}
	case uninstall:
		if err = serviceUninstall(); err == nil {
			log.Printf("Serviciul %s a fost dezinstalat.", ServiceName)
		}
	case start:
		if err = serviceStart(); err == nil {
			log.Printf("Serviciul %s a pornit.", ServiceName)
		}
	case stop:
		if err = serviceStop(); err == nil {
			log.Printf("Serviciul %s a fost oprit.", ServiceName)
		}
	}
	if err != nil {
		log.Printf("EROARE: %v", err)
		return 1
	}
	return 0
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

func registerRoutes(mux *http.ServeMux) {
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))

	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		b, _ := webFS.ReadFile("web/login.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	mux.HandleFunc("POST /api/login", handleLogin)
	mux.HandleFunc("POST /api/logout", handleLogout)

	api := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, requireAuth(h)) }

	api("GET /api/system", handleSystem)
	api("POST /api/system/test-hw", handleTestHW)
	api("POST /api/system/shutdown", handleShutdown)
	api("GET /api/service", handleServiceStatus)
	api("POST /api/service/{action}", handleServiceAction)
	api("GET /api/settings", handleGetSettings)
	api("POST /api/settings", handleSaveSettings)

	api("GET /api/fs", handleFS)
	api("POST /api/fs/mkdir", handleMkdir)
	api("POST /api/upload", handleUpload)
	api("POST /api/reveal", handleReveal)
	api("GET /api/probe", handleProbe)
	api("POST /api/preview", handlePreview)

	api("GET /api/jobs", handleListJobs)
	api("POST /api/jobs", handleCreateJobs)
	api("POST /api/jobs/clear", handleClearJobs)
	api("POST /api/queue/pause", handleQueuePause)
	api("GET /api/jobs/{id}", handleGetJob)
	api("DELETE /api/jobs/{id}", handleDeleteJob)
	api("POST /api/jobs/{id}/{action}", handleJobAction)
	api("GET /api/jobs/{id}/download", handleJobDownload)

	api("GET /api/ui-state", handleGetUIState)
	api("POST /api/ui-state", handleSaveUIState)
	api("GET /api/presets", handleListPresets)
	api("POST /api/presets", handleSavePreset)
	api("DELETE /api/presets/{name}", handleDeletePreset)

	api("POST /api/ffmpeg/download", handleFFmpegDownload)
	api("GET /api/ffmpeg/download", handleFFmpegDownloadStatus)

	mux.Handle("/", requireAuthPage(static))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func lanIPs() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}
