//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	createNoWindow      = 0x08000000
	belowNormalPriority = 0x00004000
	processSuspendRes   = 0x0800
)

var (
	ntdll       = syscall.NewLazyDLL("ntdll.dll")
	procSuspend = ntdll.NewProc("NtSuspendProcess")
	procResume  = ntdll.NewProc("NtResumeProcess")
)

func setProcAttrs(cmd *exec.Cmd, low bool) {
	flags := uint32(createNoWindow)
	if low {
		flags |= belowNormalPriority
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
}

func ntCall(p *os.Process, proc *syscall.LazyProc) error {
	h, err := syscall.OpenProcess(processSuspendRes, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	r, _, _ := proc.Call(uintptr(h))
	if r != 0 {
		return fmt.Errorf("NTSTATUS 0x%x", r)
	}
	return nil
}

func suspendProcess(p *os.Process) error { return ntCall(p, procSuspend) }
func resumeProcess(p *os.Process) error  { return ntCall(p, procResume) }

func listRoots() []string {
	var out []string
	for c := 'A'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if _, err := os.Stat(d); err == nil {
			out = append(out, d)
		}
	}
	return out
}

// openWindow deschide interfața locală într-o fereastră de tip aplicație (Edge/Chrome --app)
func openWindow(url string) {
	cands := []string{}
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		b := os.Getenv(env)
		if b == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(b, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(b, `Google\Chrome\Application\chrome.exe`))
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			cmd := exec.Command(c, "--app="+url, "--window-size=1400,900")
			if cmd.Start() == nil {
				return
			}
		}
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func revealFile(p string) {
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		exec.Command("explorer", p).Start()
		return
	}
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + p + `"`}
	cmd.Start()
}

// ensureFFmpegFirewall permite traficul de intrare (UDP multicast, RTP, SRT) pentru ffmpeg.exe.
// Funcționează când rulăm ca serviciu / administrator; altfel netsh eșuează silențios.
func ensureFFmpegFirewall(ff string) {
	if !filepath.IsAbs(ff) {
		return
	}
	run := func(name string, args ...string) error {
		c := exec.Command(name, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
		return c.Run()
	}
	progs := []string{ff, filepath.Join(filepath.Dir(ff), "ffprobe.exe")}
	// Windows creează reguli de BLOCARE dacă promptul firewall a fost închis / anulat;
	// blocarea are prioritate față de permisiune -> le ștergem
	for _, p := range progs {
		ps := "Get-NetFirewallApplicationFilter -Program '" + strings.ReplaceAll(p, "'", "''") + "' -ErrorAction SilentlyContinue | " +
			"Get-NetFirewallRule | Where-Object { $_.Action -eq 'Block' } | Remove-NetFirewallRule"
		run("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	}
	run("netsh", "advfirewall", "firewall", "delete", "rule", "name=VideoEncoder FFmpeg")
	ok := true
	for _, p := range progs {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := run("netsh", "advfirewall", "firewall", "add", "rule", "name=VideoEncoder FFmpeg", "dir=in", "action=allow",
			"program="+p, "enable=yes", "profile=any"); err != nil {
			ok = false
		}
	}
	if ok {
		log.Printf("firewall: trafic de intrare permis pentru %s (+ffprobe)", ff)
	} else {
		log.Printf("firewall: nu pot adăuga regula (rulează ca serviciu sau administrator)")
	}
}
