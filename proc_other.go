//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func setProcAttrs(cmd *exec.Cmd, low bool) {}

func suspendProcess(p *os.Process) error { return p.Signal(syscall.SIGSTOP) }
func resumeProcess(p *os.Process) error  { return p.Signal(syscall.SIGCONT) }

func listRoots() []string { return []string{"/"} }

func openWindow(url string) {
	if runtime.GOOS == "darwin" {
		exec.Command("open", url).Start()
		return
	}
	exec.Command("xdg-open", url).Start()
}

func revealFile(p string) {
	exec.Command("xdg-open", p).Start()
}
