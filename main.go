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
const AppVersion = "1.0.0"

//go:embed web
var webFS embed.FS

type Config struct {
	Port        int    `json:"port"`
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
	flag.Parse()
	if *dataDir != "" {
		baseDir = *dataDir
	}
	os.MkdirAll(baseDir, 0755)

	lf, err := os.OpenFile(filepath.Join(baseDir, "videoencoder.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stdout, lf))
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

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// probabil rulează deja – deschidem doar fereastra
		log.Printf("nu pot asculta pe %s: %v", addr, err)
		if !*noGUI {
			openWindow(localURL)
		}
		time.Sleep(2 * time.Second)
		os.Exit(1)
	}

	jobs = NewJobManager()
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
	if c.OpenWindow && !*noGUI {
		go func() { time.Sleep(400 * time.Millisecond); openWindow(localURL) }()
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
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
