//go:build windows

package main

import (
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	pGetSystemTimes     = kernel32.NewProc("GetSystemTimes")
	pGlobalMemoryStatus = kernel32.NewProc("GlobalMemoryStatusEx")
	psapi               = syscall.NewLazyDLL("psapi.dll")
	pGetProcessMemInfo  = psapi.NewProc("GetProcessMemoryInfo")
	pdh                 = syscall.NewLazyDLL("pdh.dll")
	pPdhOpenQuery       = pdh.NewProc("PdhOpenQueryW")
	pPdhAddEnglish      = pdh.NewProc("PdhAddEnglishCounterW")
	pPdhCollect         = pdh.NewProc("PdhCollectQueryData")
	pPdhGetFormattedArr = pdh.NewProc("PdhGetFormattedCounterArrayW")
)

const (
	processQueryLimited = 0x1000
	pdhFmtDouble        = 0x00000200
	pdhMoreData         = 0x800007D2
)

type memStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type procMemCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type pdhItem struct {
	name   *uint16
	status uint32
	_      uint32
	value  float64
}

type sysMonitor struct {
	lastIdle, lastKernel, lastUser uint64
	lastProc                       map[int]uint64
	lastAt                         time.Time
	gpuQuery, gpuCounter           uintptr
	gpuOK                          bool
}

func ft(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

func newSysMonitor() *sysMonitor {
	m := &sysMonitor{lastProc: map[int]uint64{}}
	// contor GPU (aceleași date ca Task Manager): \GPU Engine(*)\Utilization Percentage
	if pdh.Load() == nil {
		var q uintptr
		if r, _, _ := pPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q))); r == 0 {
			var c uintptr
			path, _ := syscall.UTF16PtrFromString(`\GPU Engine(*)\Utilization Percentage`)
			if r, _, _ := pPdhAddEnglish.Call(q, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&c))); r == 0 {
				m.gpuQuery, m.gpuCounter, m.gpuOK = q, c, true
				pPdhCollect.Call(q)
			}
		}
	}
	return m
}

func procCPUTime(pid int) (uint64, uint64, bool) {
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return 0, 0, false
	}
	defer syscall.CloseHandle(h)
	var c, e, k, u syscall.Filetime
	if syscall.GetProcessTimes(h, &c, &e, &k, &u) != nil {
		return 0, 0, false
	}
	var mc procMemCounters
	mc.cb = uint32(unsafe.Sizeof(mc))
	pGetProcessMemInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mc)), uintptr(mc.cb))
	return ft(k) + ft(u), uint64(mc.WorkingSetSize), true
}

// gpuSample: utilizare pe tip de motor (sumă pe instanțe) + pe proces
func (m *sysMonitor) gpuSample() (map[string]float64, map[int]float64) {
	if !m.gpuOK {
		return nil, nil
	}
	if r, _, _ := pPdhCollect.Call(m.gpuQuery); r != 0 {
		return nil, nil
	}
	var size, count uint32
	r, _, _ := pPdhGetFormattedArr.Call(m.gpuCounter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	r, _, _ = pPdhGetFormattedArr.Call(m.gpuCounter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return nil, nil
	}
	items := unsafe.Slice((*pdhItem)(unsafe.Pointer(&buf[0])), count)
	eng := map[string]float64{}
	perPid := map[int]map[string]float64{}
	for _, it := range items {
		if it.status != 0 && it.status != 1 { // PDH_CSTATUS_VALID_DATA / NEW_DATA
			continue
		}
		name := wstr(it.name)
		// pid_1234_luid_0x..._phys_0_eng_3_engtype_VideoDecode
		typ := "Altele"
		if i := strings.Index(name, "engtype_"); i >= 0 {
			typ = name[i+8:]
		}
		eng[typ] += it.value
		if strings.HasPrefix(name, "pid_") {
			rest := name[4:]
			if k := strings.IndexByte(rest, '_'); k > 0 {
				if pid, err := strconv.Atoi(rest[:k]); err == nil {
					if perPid[pid] == nil {
						perPid[pid] = map[string]float64{}
					}
					perPid[pid][typ] += it.value
				}
			}
		}
	}
	for k, v := range eng {
		eng[k] = clampPct(v)
	}
	pg := map[int]float64{}
	for pid, mm := range perPid {
		mx := 0.0
		for _, v := range mm {
			if v > mx {
				mx = v
			}
		}
		pg[pid] = clampPct(mx)
	}
	return eng, pg
}

func (m *sysMonitor) sample(pids []int) SysStat {
	st := SysStat{Procs: map[int]ProcStat{}}
	now := time.Now()
	var idle, kern, user syscall.Filetime
	if r, _, _ := pGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kern)), uintptr(unsafe.Pointer(&user))); r != 0 {
		i, k, u := ft(idle), ft(kern), ft(user)
		if m.lastKernel > 0 {
			di, dk, du := float64(i-m.lastIdle), float64(k-m.lastKernel), float64(u-m.lastUser)
			if dk+du > 0 {
				st.CPU = clampPct((1 - di/(dk+du)) * 100)
			}
		}
		m.lastIdle, m.lastKernel, m.lastUser = i, k, u
	}
	ms := memStatusEx{Length: 64}
	if r, _, _ := pGlobalMemoryStatus.Call(uintptr(unsafe.Pointer(&ms))); r != 0 {
		st.MemTotal = ms.TotalPhys
		st.MemUsed = ms.TotalPhys - ms.AvailPhys
	}
	eng, pg := m.gpuSample()
	if eng != nil {
		st.HasGPU = true
		st.GPUEng = eng
		for _, v := range eng {
			if v > st.GPU {
				st.GPU = v
			}
		}
	}
	elapsed := now.Sub(m.lastAt).Seconds()
	cores := float64(runtimeCores())
	newProc := map[int]uint64{}
	for _, pid := range pids {
		t, mem, ok := procCPUTime(pid)
		if !ok {
			continue
		}
		newProc[pid] = t
		ps := ProcStat{Mem: mem, GPU: pg[pid]}
		if prev, ok := m.lastProc[pid]; ok && elapsed > 0 && t >= prev {
			ps.CPU = clampPct(float64(t-prev) / 1e7 / elapsed / cores * 100)
		}
		st.Procs[pid] = ps
	}
	m.lastProc = newProc
	m.lastAt = now
	return st
}

// wstr citește un șir UTF-16 terminat cu 0 (fără a citi dincolo de terminator)
func wstr(p *uint16) string {
	if p == nil {
		return ""
	}
	var u []uint16
	for i := 0; i < 1024; i++ {
		c := *(*uint16)(unsafe.Add(unsafe.Pointer(p), uintptr(i)*2))
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return syscall.UTF16ToString(u)
}
