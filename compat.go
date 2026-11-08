package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// FFmpeg compatibil pentru plăci video vechi.
//
// Plăcile vechi au drivere finale care nu mai merg cu FFmpeg-ul nou:
//   - NVIDIA Kepler (GT 630/640/710/730, GTX 6xx/7xx) – driver max. 474.x, NVENC API 11.0/11.1
//   - Intel HD 2000/2500/3000/4000 (Sandy/Ivy Bridge) – runtime Media SDK vechi
//   - AMD VCE 1.0–3.x (ex. Fujitsu Futro S930: GX-415GA/GX-424CC, HD 7000–R9 300) – runtime AMF vechi
//
// Dacă PC-ul are o placă video dar niciun encoder hardware nu merge cu FFmpeg-ul curent,
// VideoEncoder descarcă automat FFmpeg 5.1.2 într-un folder separat, îl testează și,
// dacă acolo merge encodarea hardware, îl folosește. Încercarea se face o singură dată
// (până la schimbarea FFmpeg-ului sau a driverului).

const compatVer = "5.1.2"
const compatURL = "https://github.com/GyanD/codexffmpeg/releases/download/5.1.2/ffmpeg-5.1.2-essentials_build.zip"

var compatMu sync.Mutex

var gpuRe = regexp.MustCompile(`(?i)nvidia|geforce|quadro|radeon|amd|ati |firepro|intel.*(hd|uhd|iris|arc)`)

// gpuNames – plăcile video din sistem (fără adaptorul generic Microsoft)
func gpuNames() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	out, err := runQuiet(20*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name + ' | ' + $_.DriverVersion }")
	if err != nil {
		return nil
	}
	var r []string
	for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && gpuRe.MatchString(l) {
			r = append(r, l)
		}
	}
	return r
}

// hwTestWith testează encoderele hardware cu un anumit ffmpeg
func hwTestWith(exe string) map[string]bool {
	res := map[string]bool{}
	out, _ := runQuiet(15*time.Second, exe, "-hide_banner", "-encoders")
	list := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && len(f[0]) == 6 {
			list[f[1]] = true
		}
	}
	for _, e := range hwEncoders {
		if !list[e] {
			continue
		}
		_, err := runQuiet(20*time.Second, exe, "-hide_banner", "-v", "error", "-f", "lavfi", "-i",
			"color=c=black:s=640x360:r=25:d=0.4", "-pix_fmt", "yuv420p", "-c:v", e, "-f", "null", "-")
		if err == nil {
			res[e] = true
		}
	}
	return res
}

// maybeCompatFFmpeg – apelat după detectarea encoderelor
func maybeCompatFFmpeg() {
	if runtime.GOOS != "windows" {
		return
	}
	if !compatMu.TryLock() {
		return
	}
	defer compatMu.Unlock()

	encMu.Lock()
	anyHW := false
	for _, ok := range hwWorks {
		anyHW = anyHW || ok
	}
	ver := ffVersion
	encMu.Unlock()
	cur := ffmpegExe()
	dir := filepath.Join(baseDir, "ffmpeg-compat")
	compatExe := filepath.Join(dir, "ffmpeg.exe")
	if anyHW || ver == "" || strings.EqualFold(filepath.Clean(cur), filepath.Clean(compatExe)) {
		return
	}
	gpus := gpuNames()
	if len(gpus) == 0 {
		return
	}
	// o singură încercare pentru aceeași combinație FFmpeg + driver
	key := ver + " || " + strings.Join(gpus, " ; ")
	marker := filepath.Join(dir, "tried.txt")
	if b, err := os.ReadFile(marker); err == nil && string(b) == key {
		return
	}
	log.Printf("Placă video găsită (%s), dar niciun encoder hardware nu merge cu %s – încerc FFmpeg %s compatibil cu drivere vechi",
		strings.Join(gpus, "; "), strings.TrimPrefix(ver, "ffmpeg version "), compatVer)

	if !fileExists(compatExe) {
		var err error
		for i := 1; i <= 5; i++ { // GitHub dă uneori 502/504 la descărcări
			if err = fetchFFmpegZip(compatURL, dir); err == nil {
				break
			}
			log.Printf("FFmpeg %s: încercarea %d eșuată (%v)", compatVer, i, err)
			time.Sleep(time.Duration(i*10) * time.Second)
		}
		if err != nil {
			log.Printf("FFmpeg %s: descărcare eșuată: %v", compatVer, err)
			return
		}
	}
	os.MkdirAll(dir, 0755)
	os.WriteFile(marker, []byte(key), 0644)
	ensureFFmpegFirewall(compatExe)
	res := hwTestWith(compatExe)
	var ok []string
	for k := range res {
		ok = append(ok, k)
	}
	if len(ok) == 0 {
		log.Printf("FFmpeg %s: nici aici nu merge encodarea hardware – verifică driverul plăcii video (%s). Rămâne encodarea pe CPU (x264).",
			compatVer, strings.Join(gpus, "; "))
		return
	}
	log.Printf("FFmpeg %s: encodare hardware funcțională %v – îl folosesc de acum (Setări → FFmpeg)", compatVer, ok)
	cfgMu.Lock()
	cfg.FFmpegPath = compatExe
	cfg.FFprobePath = filepath.Join(dir, "ffprobe.exe")
	saveConfig()
	cfgMu.Unlock()
	go detectEncoders()
}

func fetchFFmpegZip(url, dir string) error {
	os.MkdirAll(dir, 0755)
	tmp := filepath.Join(dir, "download.zip")
	defer os.Remove(tmp)
	cl := &http.Client{Timeout: 15 * time.Minute}
	resp, err := cl.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		return err
	}
	zr, err := zip.OpenReader(tmp)
	if err != nil {
		return err
	}
	defer zr.Close()
	found := 0
	for _, zf := range zr.File {
		base := filepath.Base(zf.Name)
		if base != "ffmpeg.exe" && base != "ffprobe.exe" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(filepath.Join(dir, base))
		if err == nil {
			_, err = io.Copy(out, rc)
			out.Close()
			if err == nil {
				found++
			}
		}
		rc.Close()
	}
	if found < 2 {
		return fmt.Errorf("arhiva nu conține ffmpeg.exe / ffprobe.exe")
	}
	return nil
}
