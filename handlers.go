package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ================= FFmpeg =================

func exeName(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

func findTool(name, configured string) string {
	if configured != "" && fileExists(configured) {
		return configured
	}
	for _, p := range []string{
		filepath.Join(baseDir, "ffmpeg", exeName(name)),
		filepath.Join(baseDir, "ffmpeg", "bin", exeName(name)),
		filepath.Join(baseDir, exeName(name)),
	} {
		if fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return exeName(name)
}

func ffmpegExe() string { return findTool("ffmpeg", getCfg().FFmpegPath) }

func ffprobeExe() string {
	c := getCfg()
	if c.FFprobePath == "" && c.FFmpegPath != "" {
		p := filepath.Join(filepath.Dir(c.FFmpegPath), exeName("ffprobe"))
		if fileExists(p) {
			return p
		}
	}
	return findTool("ffprobe", c.FFprobePath)
}

func runQuiet(timeout time.Duration, exe string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	setProcAttrs(cmd, false)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// runSplit – stdout și stderr separate (ffprobe scrie avertismente pe stderr)
func runSplit(timeout time.Duration, exe string, args ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	setProcAttrs(cmd, false)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

func probeDuration(path string) float64 {
	out, _, err := runSplit(30*time.Second, ffprobeExe(), "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path)
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(out), "\n") {
		if v, err := strconv.ParseFloat(strings.TrimSpace(l), 64); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

var (
	encMu      sync.Mutex
	encList    = map[string]bool{} // encoder prezent în build
	hwWorks    = map[string]bool{} // encoder HW testat funcțional
	hwTested   bool
	amfHighBF  bool // h264_amf acceptă profil High + B-frames
	ffVersion  string
	hwEncoders = []string{"h264_nvenc", "hevc_nvenc", "av1_nvenc", "h264_qsv", "hevc_qsv", "av1_qsv", "h264_amf", "hevc_amf", "av1_amf"}
)

func detectEncoders() {
	exe := ffmpegExe()
	go ensureFFmpegFirewall(exe)
	out, err := runQuiet(15*time.Second, exe, "-hide_banner", "-version")
	encMu.Lock()
	if err != nil {
		ffVersion = ""
		encList = map[string]bool{}
		encMu.Unlock()
		log.Printf("FFmpeg nu a fost găsit (%s). Îl poți descărca din Setări.", exe)
		return
	}
	ffVersion = strings.SplitN(string(out), "\n", 2)[0]
	encMu.Unlock()

	out, _ = runQuiet(15*time.Second, exe, "-hide_banner", "-encoders")
	list := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && len(f[0]) == 6 {
			list[f[1]] = true
		}
	}
	encMu.Lock()
	encList = list
	encMu.Unlock()
	log.Printf("FFmpeg: %s", ffVersion)
	testHW()
}

func testHW() {
	exe := ffmpegExe()
	res := map[string]bool{}
	for _, e := range hwEncoders {
		encMu.Lock()
		present := encList[e]
		encMu.Unlock()
		if !present {
			continue
		}
		_, err := runQuiet(20*time.Second, exe, "-hide_banner", "-v", "error", "-f", "lavfi", "-i",
			"color=c=black:s=640x360:r=25:d=0.4", "-pix_fmt", "yuv420p", "-c:v", e, "-f", "null", "-")
		res[e] = err == nil
	}
	// AMD: High + B-frames (VCE 2.0+, ex. Futro S930) – structură ca la x264, mult mai multă calitate la bitrate mic
	amfHB := false
	if res["h264_amf"] {
		_, err := runQuiet(20*time.Second, exe, "-hide_banner", "-v", "error", "-f", "lavfi", "-i",
			"testsrc2=s=640x360:r=25:d=1", "-pix_fmt", "nv12", "-c:v", "h264_amf", "-profile:v", "high", "-bf", "2", "-f", "null", "-")
		amfHB = err == nil
	}
	encMu.Lock()
	hwWorks = res
	hwTested = true
	amfHighBF = amfHB
	encMu.Unlock()
	var ok []string
	for k, v := range res {
		if v {
			ok = append(ok, k)
		}
	}
	sort.Strings(ok)
	log.Printf("Encodere hardware funcționale: %v", ok)
	go maybeCompatFFmpeg() // plăci video vechi: încearcă automat FFmpeg 5.1.2
}

// ================= Sistem / setări =================

func handleSystem(w http.ResponseWriter, r *http.Request) {
	c := getCfg()
	encMu.Lock()
	el := make([]string, 0)
	for _, n := range []string{"libx264", "libx265", "libsvtav1", "libvpx-vp9", "prores_ks", "mpeg2video", "mpeg4", "dnxhd",
		"aac", "libfdk_aac", "libmp3lame", "ac3", "eac3", "libopus", "flac", "pcm_s16le", "mp2"} {
		if encList[n] {
			el = append(el, n)
		}
	}
	hw := map[string]bool{}
	for k, v := range hwWorks {
		hw[k] = v
	}
	ver, tested := ffVersion, hwTested
	encMu.Unlock()
	user, _ := currentUser(r)
	writeJSON(w, map[string]any{
		"app": AppName, "version": AppVersion, "os": runtime.GOOS, "cpus": runtime.NumCPU(),
		"ffmpeg": ffmpegExe(), "ffprobe": ffprobeExe(), "ffmpeg_version": ver,
		"encoders": el, "hw": hw, "hw_tested": tested,
		"local": isLocal(r), "user": user, "port": c.Port, "lan_ips": lanIPs(),
		"default_pass": c.DefaultPass, "output_dir": c.OutputDir, "base_dir": baseDir,
		"sep": string(os.PathSeparator), "is_service": isService,
	})
}

func handleTestHW(w http.ResponseWriter, r *http.Request) {
	detectEncoders()
	handleSystem(w, r)
}

func handleShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		log.Printf("oprire cerută din interfață")
		if isService {
			stopSelfService()
			return
		}
		jobs.KillAll()
		os.Exit(0)
	}()
}

func handleGetSettings(w http.ResponseWriter, r *http.Request) {
	c := getCfg()
	writeJSON(w, map[string]any{
		"port": c.Port, "bind": c.Bind, "username": c.Username, "local_no_auth": c.LocalNoAuth,
		"ffmpeg_path": c.FFmpegPath, "ffprobe_path": c.FFprobePath, "output_dir": c.OutputDir,
		"max_jobs": c.MaxJobs, "low_priority": c.LowPriority, "open_window": c.OpenWindow,
		"default_pass": c.DefaultPass, "qsv_mode_ok": c.QSVModeOK, "mcast_iface": c.McastIface,
		"ifaces": ifaceList(),
	})
}

func handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port        int     `json:"port"`
		Bind        string  `json:"bind"`
		Username    string  `json:"username"`
		NewPassword string  `json:"new_password"`
		LocalNoAuth *bool   `json:"local_no_auth"`
		FFmpegPath  *string `json:"ffmpeg_path"`
		FFprobePath *string `json:"ffprobe_path"`
		OutputDir   string  `json:"output_dir"`
		MaxJobs     int     `json:"max_jobs"`
		LowPriority *bool   `json:"low_priority"`
		OpenWindow  *bool   `json:"open_window"`
		QSVModeOK   *string `json:"qsv_mode_ok"`
		McastIface  *string `json:"mcast_iface"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "cerere invalidă")
		return
	}
	// dezactivarea accesului local fără parolă se poate face doar local
	restart := false
	cfgMu.Lock()
	if req.Port > 0 && req.Port < 65536 && req.Port != cfg.Port {
		cfg.Port = req.Port
		restart = true
	}
	if req.Bind != "" && req.Bind != cfg.Bind {
		cfg.Bind = req.Bind
		restart = true
	}
	if u := strings.TrimSpace(req.Username); u != "" {
		cfg.Username = u
	}
	if req.NewPassword != "" {
		if len(req.NewPassword) < 4 {
			cfgMu.Unlock()
			writeErr(w, 400, "parola trebuie să aibă minim 4 caractere")
			return
		}
		cfg.Salt = randToken(16)
		cfg.PassHash = hashPass(req.NewPassword, cfg.Salt)
		cfg.DefaultPass = false
	}
	if req.LocalNoAuth != nil {
		cfg.LocalNoAuth = *req.LocalNoAuth
	}
	ffChanged := false
	if req.FFmpegPath != nil && *req.FFmpegPath != cfg.FFmpegPath {
		cfg.FFmpegPath = strings.Trim(*req.FFmpegPath, `" `)
		ffChanged = true
	}
	if req.FFprobePath != nil && *req.FFprobePath != cfg.FFprobePath {
		cfg.FFprobePath = strings.Trim(*req.FFprobePath, `" `)
		ffChanged = true
	}
	if d := strings.TrimSpace(req.OutputDir); d != "" {
		cfg.OutputDir = d
		os.MkdirAll(d, 0755)
	}
	if req.MaxJobs > 0 && req.MaxJobs <= 16 {
		cfg.MaxJobs = req.MaxJobs
	}
	if req.LowPriority != nil {
		cfg.LowPriority = *req.LowPriority
	}
	if req.OpenWindow != nil {
		cfg.OpenWindow = *req.OpenWindow
	}
	if req.McastIface != nil {
		cfg.McastIface = strings.TrimSpace(*req.McastIface)
	}
	if req.QSVModeOK != nil {
		switch *req.QSVModeOK {
		case "", "d3d11", "dxva2", "hybrid", "cpudec":
			cfg.QSVModeOK = *req.QSVModeOK
		}
	}
	saveConfig()
	cfgMu.Unlock()
	if req.NewPassword != "" {
		dropAllSessions()
	}
	if ffChanged {
		go detectEncoders()
	}
	jobs.kick()
	writeJSON(w, map[string]any{"ok": true, "restart": restart})
}

// ================= Fișiere =================

var mediaExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(".mp4 .mkv .mov .avi .ts .m2ts .mts .mpg .mpeg .vob .wmv .flv .webm .m4v .3gp .ogv .mxf .dv .rm .rmvb .divx .f4v .y4m .m3u8 " +
		".mp3 .aac .m4a .flac .wav .ogg .opus .wma .ac3 .eac3 .dts .mp2 .aiff .alac .mka .gif .srt .ass") {
		mediaExt[e] = true
	}
}

type fsEntry struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	Dir   bool      `json:"dir"`
	Size  int64     `json:"size"`
	Mod   time.Time `json:"mod"`
	Media bool      `json:"media"`
}

func handleFS(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	all := r.URL.Query().Get("all") == "1"
	if p == "" {
		var roots []fsEntry
		for _, d := range listRoots() {
			roots = append(roots, fsEntry{Name: d, Path: d, Dir: true})
		}
		writeJSON(w, map[string]any{"path": "", "parent": "", "entries": roots,
			"shortcuts": shortcuts()})
		return
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" && len(p) == 2 && p[1] == ':' {
		p += `\`
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var out []fsEntry
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, "$") || name == "System Volume Information" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		isDir := info.IsDir()
		ext := strings.ToLower(filepath.Ext(name))
		media := !isDir && mediaExt[ext]
		if !isDir && !media && !all {
			continue
		}
		out = append(out, fsEntry{Name: name, Path: filepath.Join(p, name), Dir: isDir, Size: info.Size(), Mod: info.ModTime(), Media: media})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Dir != out[b].Dir {
			return out[a].Dir
		}
		return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name)
	})
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	writeJSON(w, map[string]any{"path": p, "parent": parent, "entries": out, "shortcuts": shortcuts()})
}

func shortcuts() []fsEntry {
	c := getCfg()
	s := []fsEntry{
		{Name: "Ieșire", Path: c.OutputDir, Dir: true},
		{Name: "Încărcate", Path: filepath.Join(baseDir, "uploads"), Dir: true},
	}
	if h, err := os.UserHomeDir(); err == nil {
		for _, n := range []string{"Videos", "Desktop", "Downloads"} {
			p := filepath.Join(h, n)
			if fileExists(p) {
				s = append(s, fsEntry{Name: n, Path: p, Dir: true})
			}
		}
	}
	return s
}

func handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path string }
	if readJSON(r, &req) != nil || req.Path == "" {
		writeErr(w, 400, "cale lipsă")
		return
	}
	if err := os.MkdirAll(req.Path, 0755); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleReveal(w http.ResponseWriter, r *http.Request) {
	if !isLocal(r) {
		writeErr(w, 403, "disponibil doar local")
		return
	}
	var req struct{ Path string }
	if readJSON(r, &req) != nil || req.Path == "" {
		writeErr(w, 400, "cale lipsă")
		return
	}
	revealFile(req.Path)
	writeJSON(w, map[string]any{"ok": true})
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, 400, "upload invalid")
		return
	}
	dir := filepath.Join(baseDir, "uploads")
	os.MkdirAll(dir, 0755)
	var saved []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if part.FileName() == "" {
			continue
		}
		name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if name == "." || name == ".." || name == "" {
			continue
		}
		dst := filepath.Join(dir, name)
		ext := filepath.Ext(name)
		for i := 1; fileExists(dst); i++ {
			dst = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i, ext))
		}
		f, err := os.Create(dst)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		_, err = io.Copy(f, part)
		f.Close()
		if err != nil {
			os.Remove(dst)
			writeErr(w, 500, err.Error())
			return
		}
		saved = append(saved, dst)
		log.Printf("upload: %s", dst)
	}
	writeJSON(w, map[string]any{"files": saved})
}

func handleProbe(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	args := []string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams"}
	if IsURL(p) {
		args = append(args, "-analyzeduration", "5000000", "-probesize", "5000000")
		p = UDPInput(p) // multicast: aceeași placă de rețea + buffer + timeout ca la encodare
	}
	args = append(args, p)
	out, stderr, err := runSplit(20*time.Second, ffprobeExe(), args...)
	if !json.Valid(out) || !bytes.Contains(out, []byte(`"format"`)) {
		msg := strings.TrimSpace(string(stderr))
		if lines := strings.Split(msg, "\n"); len(lines) > 3 {
			msg = strings.Join(lines[len(lines)-3:], "\n")
		}
		if err != nil {
			msg += " (" + err.Error() + ")"
		}
		if strings.HasPrefix(strings.ToLower(p), "udp://") || strings.HasPrefix(strings.ToLower(p), "rtp://") {
			msg += " — multicast: verifică Setări → Placa de rețea pentru multicast, firewall-ul și dacă sursa e activă (comanda: " + p + ")"
		}
		writeErr(w, 400, "ffprobe: "+msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// ================= Joburi =================

func handlePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input   string         `json:"input"`
		Options *EncodeOptions `json:"options"`
	}
	if readJSON(r, &req) != nil || req.Options == nil {
		writeErr(w, 400, "cerere invalidă")
		return
	}
	in := req.Input
	if in == "" {
		in = filepath.Join("C:\\Video", "exemplu.mp4")
	}
	out := OutputPath(in, req.Options)
	writeJSON(w, map[string]any{"output": out, "cmd": CommandString(ffmpegExe(), BuildArgs(in, out, req.Options))})
}

func handleListJobs(w http.ResponseWriter, r *http.Request) {
	list, paused := jobs.List()
	writeJSON(w, map[string]any{"jobs": list, "paused": paused, "sys": sysSnapshot()})
}

func handleCreateJobs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Inputs  []string       `json:"inputs"`
		Options *EncodeOptions `json:"options"`
		Preset  string         `json:"preset"`
	}
	if readJSON(r, &req) != nil || req.Options == nil || len(req.Inputs) == 0 {
		writeErr(w, 400, "selectează cel puțin un fișier")
		return
	}
	if req.Options.IsStream() {
		u := strings.TrimSpace(req.Options.StreamURL)
		if !IsURL(u) {
			writeErr(w, 400, "introdu URL-ul de ieșire pentru stream (ex: rtmp://server/live/cheie)")
			return
		}
		if len(req.Inputs) > 1 {
			writeErr(w, 400, "pentru stream selectează o singură sursă (selecția curentă e prima din listă)")
			return
		}
	}
	var created []*Job
	for _, in := range req.Inputs {
		in = strings.Trim(strings.TrimSpace(in), `"`)
		if in == "" {
			continue
		}
		isURL := IsURL(in)
		if !isURL && !fileExists(in) {
			writeErr(w, 400, "fișier inexistent: "+in)
			return
		}
		created = append(created, jobs.Add(in, req.Options, req.Preset))
	}
	writeJSON(w, map[string]any{"jobs": created})
}

func handleGetJob(w http.ResponseWriter, r *http.Request) {
	j := jobs.Get(r.PathValue("id"), true)
	if j == nil {
		writeErr(w, 404, "job inexistent")
		return
	}
	writeJSON(w, j)
}

func handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if err := jobs.Delete(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleJobAction(w http.ResponseWriter, r *http.Request) {
	if err := jobs.Action(r.PathValue("id"), r.PathValue("action")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleClearJobs(w http.ResponseWriter, r *http.Request) {
	jobs.ClearFinished()
	writeJSON(w, map[string]any{"ok": true})
}

func handleQueuePause(w http.ResponseWriter, r *http.Request) {
	var req struct{ Paused bool }
	readJSON(r, &req)
	jobs.SetPaused(req.Paused)
	writeJSON(w, map[string]any{"ok": true, "paused": req.Paused})
}

func handleJobDownload(w http.ResponseWriter, r *http.Request) {
	j := jobs.Get(r.PathValue("id"), false)
	if j == nil || j.Status != "done" {
		writeErr(w, 404, "fișier indisponibil")
		return
	}
	if j.Options.Container == "hls" {
		// arhivăm directorul HLS
		dir := filepath.Dir(j.Output)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, filepath.Base(dir)))
		zw := zip.NewWriter(w)
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			f, err := os.Open(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			zf, _ := zw.CreateHeader(&zip.FileHeader{Name: e.Name(), Method: zip.Store})
			io.Copy(zf, f)
			f.Close()
		}
		zw.Close()
		return
	}
	f, err := os.Open(j.Output)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filepath.Base(j.Output), `"`, "")))
	http.ServeContent(w, r, filepath.Base(j.Output), st.ModTime(), f)
}

// ================= Presetări utilizator =================

var presetMu sync.Mutex

func presetsFile() string { return filepath.Join(baseDir, "presets.json") }

func loadPresets() map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	if b, err := os.ReadFile(presetsFile()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func handleListPresets(w http.ResponseWriter, r *http.Request) {
	presetMu.Lock()
	defer presetMu.Unlock()
	writeJSON(w, loadPresets())
}

func handleSavePreset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string          `json:"name"`
		Options json.RawMessage `json:"options"`
	}
	if readJSON(r, &req) != nil || strings.TrimSpace(req.Name) == "" || len(req.Options) == 0 {
		writeErr(w, 400, "nume sau opțiuni lipsă")
		return
	}
	presetMu.Lock()
	defer presetMu.Unlock()
	m := loadPresets()
	m[strings.TrimSpace(req.Name)] = req.Options
	b, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(presetsFile(), b, 0644)
	writeJSON(w, map[string]any{"ok": true})
}

func handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	presetMu.Lock()
	defer presetMu.Unlock()
	m := loadPresets()
	delete(m, r.PathValue("name"))
	b, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(presetsFile(), b, 0644)
	writeJSON(w, map[string]any{"ok": true})
}

// ================= Descărcare FFmpeg (Windows) =================

var (
	dlMu     sync.Mutex
	dlState  = map[string]any{"state": "idle"}
	dlActive bool
)

func setDL(kv map[string]any) {
	dlMu.Lock()
	for k, v := range kv {
		dlState[k] = v
	}
	dlMu.Unlock()
}

func handleFFmpegDownloadStatus(w http.ResponseWriter, r *http.Request) {
	dlMu.Lock()
	defer dlMu.Unlock()
	writeJSON(w, dlState)
}

func handleFFmpegDownload(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		writeErr(w, 400, "descărcarea automată e disponibilă doar pe Windows; instalează ffmpeg din managerul de pachete")
		return
	}
	dlMu.Lock()
	if dlActive {
		dlMu.Unlock()
		writeErr(w, 400, "descărcare deja în curs")
		return
	}
	dlActive = true
	dlState = map[string]any{"state": "downloading", "done": 0, "total": 0}
	dlMu.Unlock()
	go downloadFFmpeg()
	writeJSON(w, map[string]any{"ok": true})
}

const ffmpegZipURL = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip"

type countWriter struct {
	f    *os.File
	done int64
	last time.Time
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.f.Write(p)
	c.done += int64(n)
	if time.Since(c.last) > 300*time.Millisecond {
		c.last = time.Now()
		setDL(map[string]any{"done": c.done})
	}
	return n, err
}

func downloadFFmpeg() {
	defer func() { dlMu.Lock(); dlActive = false; dlMu.Unlock() }()
	fail := func(err error) {
		log.Printf("descărcare ffmpeg eșuată: %v", err)
		setDL(map[string]any{"state": "error", "error": err.Error()})
	}
	dir := filepath.Join(baseDir, "ffmpeg")
	os.MkdirAll(dir, 0755)
	tmp := filepath.Join(dir, "download.zip")
	resp, err := http.Get(ffmpegZipURL)
	if err != nil {
		fail(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(fmt.Errorf("HTTP %d", resp.StatusCode))
		return
	}
	setDL(map[string]any{"total": resp.ContentLength})
	f, err := os.Create(tmp)
	if err != nil {
		fail(err)
		return
	}
	cw := &countWriter{f: f}
	_, err = io.Copy(cw, resp.Body)
	f.Close()
	if err != nil {
		fail(err)
		return
	}
	setDL(map[string]any{"state": "extracting", "done": cw.done})
	zr, err := zip.OpenReader(tmp)
	if err != nil {
		fail(err)
		return
	}
	found := 0
	for _, zf := range zr.File {
		base := filepath.Base(zf.Name)
		if base != "ffmpeg.exe" && base != "ffprobe.exe" && base != "ffplay.exe" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(filepath.Join(dir, base))
		if err == nil {
			io.Copy(out, rc)
			out.Close()
			found++
		}
		rc.Close()
	}
	zr.Close()
	os.Remove(tmp)
	if found < 2 {
		fail(fmt.Errorf("arhiva nu conține ffmpeg.exe / ffprobe.exe"))
		return
	}
	cfgMu.Lock()
	cfg.FFmpegPath = filepath.Join(dir, "ffmpeg.exe")
	cfg.FFprobePath = filepath.Join(dir, "ffprobe.exe")
	saveConfig()
	cfgMu.Unlock()
	setDL(map[string]any{"state": "done"})
	log.Printf("FFmpeg instalat în %s", dir)
	detectEncoders()
}

// ================= Starea formularului (persistentă) =================

var uiMu sync.Mutex

func uiStateFile() string { return filepath.Join(baseDir, "ui_state.json") }

func handleGetUIState(w http.ResponseWriter, r *http.Request) {
	uiMu.Lock()
	defer uiMu.Unlock()
	b, err := os.ReadFile(uiStateFile())
	if err != nil || !json.Valid(b) {
		writeJSON(w, map[string]any{})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func handleSaveUIState(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || !json.Valid(b) {
		writeErr(w, 400, "stare invalidă")
		return
	}
	uiMu.Lock()
	tmp := uiStateFile() + ".tmp"
	if os.WriteFile(tmp, b, 0644) == nil {
		os.Rename(tmp, uiStateFile())
	}
	uiMu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

// ifaceList – plăcile de rețea cu IPv4 (pentru alegerea interfeței multicast)
func ifaceList() []map[string]string {
	var out []map[string]string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, map[string]string{"ip": ipn.IP.String(), "name": i.Name})
			}
		}
	}
	return out
}
