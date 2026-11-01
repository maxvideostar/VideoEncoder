//go:build windows

package main

// Serviciu Windows nativ (advapi32), fără dependențe externe.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const ServiceName = "VideoEncoder"
const serviceDisplay = "VideoEncoder (FFmpeg)"
const serviceDesc = "Encodare video FFmpeg cu interfață web locală și externă – github.com/maxvideostar/VideoEncoder"

var (
	advapi32         = syscall.NewLazyDLL("advapi32.dll")
	shell32          = syscall.NewLazyDLL("shell32.dll")
	pStartDispatcher = advapi32.NewProc("StartServiceCtrlDispatcherW")
	pRegHandlerEx    = advapi32.NewProc("RegisterServiceCtrlHandlerExW")
	pSetStatus       = advapi32.NewProc("SetServiceStatus")
	pOpenSCM         = advapi32.NewProc("OpenSCManagerW")
	pCreateSvc       = advapi32.NewProc("CreateServiceW")
	pOpenSvc         = advapi32.NewProc("OpenServiceW")
	pDeleteSvc       = advapi32.NewProc("DeleteService")
	pStartSvc        = advapi32.NewProc("StartServiceW")
	pControlSvc      = advapi32.NewProc("ControlService")
	pQuerySvc        = advapi32.NewProc("QueryServiceStatus")
	pCloseSvc        = advapi32.NewProc("CloseServiceHandle")
	pChangeCfg2      = advapi32.NewProc("ChangeServiceConfig2W")
	pShellExecute    = shell32.NewProc("ShellExecuteW")
)

const (
	scManagerConnect   = 0x0001
	scManagerAll       = 0xF003F
	svcAllAccess       = 0xF01FF
	svcQueryStatus     = 0x0004
	svcWin32Own        = 0x10
	svcAutoStart       = 2
	svcErrorNormal     = 1
	stStopped          = 1
	stStartPending     = 2
	stStopPending      = 3
	stRunning          = 4
	ctlStop            = 1
	ctlInterrogate     = 4
	ctlShutdown        = 5
	acceptStop         = 1
	acceptShutdown     = 4
	errSvcNotExist     = 1060
	errSvcNotActive    = 1062
	errSvcAlreadyRun   = 1056
	errSvcExists       = 1073
	cfgDescription     = 1
	cfgFailureActions  = 2
	scActionRestart    = 1
	errNotServiceCtrlr = 1063
)

type svcStatus struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

type svcTableEntry struct {
	name *uint16
	proc uintptr
}

type svcDescription struct{ desc *uint16 }

type scAction struct{ Type, Delay uint32 }

type svcFailureActions struct {
	ResetPeriod uint32
	RebootMsg   *uint16
	Command     *uint16
	Actions     uint32
	lpActions   *scAction
}

var (
	isService     bool
	svcHandle     uintptr
	svcStopCh     = make(chan struct{})
	svcStopOnce   sync.Once
	svcOnStop     func()
	svcHandlerCB  uintptr
	svcMainCB     uintptr
	svcCheckpoint uint32
)

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func setSvcStatus(state, accepts, wait uint32) {
	if svcHandle == 0 {
		return
	}
	svcCheckpoint++
	st := svcStatus{ServiceType: svcWin32Own, CurrentState: state, ControlsAccepted: accepts, WaitHint: wait, CheckPoint: svcCheckpoint}
	if state == stRunning || state == stStopped {
		st.CheckPoint = 0
	}
	pSetStatus.Call(svcHandle, uintptr(unsafe.Pointer(&st)))
}

// runService – blochează până la oprirea serviciului. onStop e apelat la Stop/Shutdown.
func runService(onStop func()) error {
	isService = true
	svcOnStop = onStop
	svcHandlerCB = syscall.NewCallback(func(ctrl, evType, evData, ctx uintptr) uintptr {
		switch ctrl {
		case ctlStop, ctlShutdown:
			setSvcStatus(stStopPending, 0, 20000)
			svcStopOnce.Do(func() { close(svcStopCh) })
		case ctlInterrogate:
		}
		return 0
	})
	svcMainCB = syscall.NewCallback(func(argc, argv uintptr) uintptr {
		h, _, _ := pRegHandlerEx.Call(uintptr(unsafe.Pointer(u16(ServiceName))), svcHandlerCB, 0)
		svcHandle = h
		setSvcStatus(stRunning, acceptStop|acceptShutdown, 0)
		log.Printf("serviciul %s rulează", ServiceName)
		<-svcStopCh
		log.Printf("serviciul se oprește…")
		if svcOnStop != nil {
			svcOnStop()
		}
		setSvcStatus(stStopped, 0, 0)
		return 0
	})
	table := []svcTableEntry{{u16(ServiceName), svcMainCB}, {nil, 0}}
	r, _, err := pStartDispatcher.Call(uintptr(unsafe.Pointer(&table[0])))
	if r == 0 {
		return err
	}
	return nil
}

// stopSelfService – oprire cerută din interfață când rulăm ca serviciu
func stopSelfService() {
	setSvcStatus(stStopPending, 0, 20000)
	svcStopOnce.Do(func() { close(svcStopCh) })
}

func openSCM(access uint32) (uintptr, error) {
	h, _, err := pOpenSCM.Call(0, 0, uintptr(access))
	if h == 0 {
		return 0, err
	}
	return h, nil
}

func errno(err error) uintptr {
	if e, ok := err.(syscall.Errno); ok {
		return uintptr(e)
	}
	return 0
}

func serviceInstall(user, pass string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.Abs(exe)
	m, err := openSCM(scManagerAll)
	if err != nil {
		return fmt.Errorf("nu am drepturi de administrator (%v)", err)
	}
	defer pCloseSvc.Call(m)
	bin := fmt.Sprintf(`"%s" -service`, exe)
	var pUser, pPass uintptr
	if user != "" {
		if !strings.Contains(user, `\`) && !strings.Contains(user, "@") {
			user = `.\` + user
		}
		pUser = uintptr(unsafe.Pointer(u16(user)))
		pPass = uintptr(unsafe.Pointer(u16(pass)))
	}
	s, _, err := pCreateSvc.Call(m, uintptr(unsafe.Pointer(u16(ServiceName))), uintptr(unsafe.Pointer(u16(serviceDisplay))),
		svcAllAccess, svcWin32Own, svcAutoStart, svcErrorNormal, uintptr(unsafe.Pointer(u16(bin))),
		0, 0, 0, pUser, pPass)
	if s == 0 {
		if errno(err) == errSvcExists {
			return fmt.Errorf("serviciul este deja instalat")
		}
		return err
	}
	defer pCloseSvc.Call(s)
	d := svcDescription{u16(serviceDesc)}
	pChangeCfg2.Call(s, cfgDescription, uintptr(unsafe.Pointer(&d)))
	acts := []scAction{{scActionRestart, 5000}, {scActionRestart, 10000}, {scActionRestart, 30000}}
	fa := svcFailureActions{ResetPeriod: 86400, Actions: uint32(len(acts)), lpActions: &acts[0]}
	pChangeCfg2.Call(s, cfgFailureActions, uintptr(unsafe.Pointer(&fa)))
	return nil
}

func openService(access uint32) (m, s uintptr, err error) {
	scmAccess := uint32(scManagerAll)
	if access == svcQueryStatus {
		scmAccess = scManagerConnect
	}
	m, err = openSCM(scmAccess)
	if err != nil {
		return 0, 0, fmt.Errorf("nu am drepturi de administrator (%v)", err)
	}
	s, _, err = pOpenSvc.Call(m, uintptr(unsafe.Pointer(u16(ServiceName))), uintptr(access))
	if s == 0 {
		pCloseSvc.Call(m)
		if errno(err) == errSvcNotExist {
			return 0, 0, fmt.Errorf("serviciul nu este instalat")
		}
		return 0, 0, err
	}
	return m, s, nil
}

func queryState(s uintptr) uint32 {
	var st svcStatus
	r, _, _ := pQuerySvc.Call(s, uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return 0
	}
	return st.CurrentState
}

func serviceStart() error {
	m, s, err := openService(svcAllAccess)
	if err != nil {
		return err
	}
	defer pCloseSvc.Call(m)
	defer pCloseSvc.Call(s)
	r, _, err := pStartSvc.Call(s, 0, 0)
	if r == 0 && errno(err) != errSvcAlreadyRun {
		return err
	}
	return nil
}

func serviceStop() error {
	m, s, err := openService(svcAllAccess)
	if err != nil {
		return err
	}
	defer pCloseSvc.Call(m)
	defer pCloseSvc.Call(s)
	var st svcStatus
	r, _, err := pControlSvc.Call(s, ctlStop, uintptr(unsafe.Pointer(&st)))
	if r == 0 && errno(err) != errSvcNotActive {
		return err
	}
	for i := 0; i < 60 && queryState(s) != stStopped; i++ {
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// serviceDeleteOnly marchează serviciul pentru ștergere (efectiv la oprire)
func serviceDeleteOnly() error {
	m, s, err := openService(svcAllAccess)
	if err != nil {
		return err
	}
	defer pCloseSvc.Call(m)
	defer pCloseSvc.Call(s)
	r, _, err := pDeleteSvc.Call(s)
	if r == 0 {
		return err
	}
	return nil
}

func serviceUninstall() error {
	serviceStop()
	m, s, err := openService(svcAllAccess)
	if err != nil {
		return err
	}
	defer pCloseSvc.Call(m)
	defer pCloseSvc.Call(s)
	r, _, err := pDeleteSvc.Call(s)
	if r == 0 {
		return err
	}
	return nil
}

// serviceState: "absent", "stopped", "starting", "stopping", "running"
func serviceState() string {
	m, s, err := openService(svcQueryStatus)
	if err != nil {
		return "absent"
	}
	defer pCloseSvc.Call(m)
	defer pCloseSvc.Call(s)
	switch queryState(s) {
	case stStopped:
		return "stopped"
	case stStartPending:
		return "starting"
	case stStopPending:
		return "stopping"
	case stRunning:
		return "running"
	}
	return "unknown"
}

// runElevated pornește exe-ul curent cu drepturi de administrator (prompt UAC)
func runElevated(args string) error {
	exe, _ := os.Executable()
	r, _, _ := pShellExecute.Call(0, uintptr(unsafe.Pointer(u16("runas"))), uintptr(unsafe.Pointer(u16(exe))),
		uintptr(unsafe.Pointer(u16(args))), uintptr(unsafe.Pointer(u16(filepath.Dir(exe)))), 0)
	if r <= 32 {
		if r == 5 {
			return fmt.Errorf("acțiune anulată (UAC refuzat)")
		}
		return fmt.Errorf("ShellExecute a eșuat (%d)", r)
	}
	return nil
}

const serviceSupported = true
