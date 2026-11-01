package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"supported":  serviceSupported,
		"state":      serviceState(),
		"is_service": isService,
		"name":       ServiceName,
	})
}

// handover: după ce serviciul pornește, instanța normală se închide ca să elibereze portul
var handoverStarted bool

func startHandover() {
	if handoverStarted {
		return
	}
	handoverStarted = true
	go func() {
		for i := 0; i < 90; i++ {
			time.Sleep(time.Second)
			if serviceState() == "running" {
				log.Printf("serviciul rulează – închid aplicația ca să preia portul")
				time.Sleep(700 * time.Millisecond)
				jobs.KillAll()
				os.Exit(0)
			}
		}
		handoverStarted = false
		log.Printf("serviciul nu a pornit în 90s – aplicația rămâne activă")
	}()
}

func handleServiceAction(w http.ResponseWriter, r *http.Request) {
	if !serviceSupported {
		writeErr(w, 400, "serviciul Windows este disponibil doar pe Windows")
		return
	}
	if !isLocal(r) {
		writeErr(w, 403, "gestionarea serviciului se face doar de pe acest calculator")
		return
	}
	action := r.PathValue("action")
	var err error
	handover := false
	switch action {
	case "install":
		if isService {
			writeErr(w, 400, "rulează deja ca serviciu")
			return
		}
		persistToolPaths()
		if err = runElevated("-install"); err == nil {
			handover = true
		}
	case "start":
		if isService {
			writeErr(w, 400, "serviciul rulează deja")
			return
		}
		persistToolPaths()
		if err = runElevated("-start"); err == nil {
			handover = true
		}
	case "stop":
		if isService {
			writeJSON(w, map[string]any{"ok": true, "stopping": true})
			go func() { time.Sleep(500 * time.Millisecond); stopSelfService() }()
			return
		}
		err = runElevated("-stop")
	case "uninstall":
		if isService {
			// LocalSystem e administrator: ștergem direct, apoi ne oprim
			writeJSON(w, map[string]any{"ok": true, "stopping": true})
			go func() {
				time.Sleep(500 * time.Millisecond)
				if e := serviceDeleteOnly(); e != nil {
					log.Printf("dezinstalare serviciu: %v", e)
				}
				stopSelfService()
			}()
			return
		}
		err = runElevated("-uninstall")
	default:
		writeErr(w, 400, "acțiune necunoscută")
		return
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if handover {
		startHandover()
	}
	writeJSON(w, map[string]any{"ok": true, "handover": handover})
}

// persistToolPaths salvează căile ffmpeg găsite în PATH-ul utilizatorului,
// pentru că serviciul (LocalSystem) are alt PATH
func persistToolPaths() {
	ff, fp := ffmpegExe(), ffprobeExe()
	cfgMu.Lock()
	defer cfgMu.Unlock()
	changed := false
	if cfg.FFmpegPath == "" && filepath.IsAbs(ff) && fileExists(ff) {
		cfg.FFmpegPath = ff
		changed = true
	}
	if cfg.FFprobePath == "" && filepath.IsAbs(fp) && fileExists(fp) {
		cfg.FFprobePath = fp
		changed = true
	}
	if changed {
		saveConfig()
	}
}
