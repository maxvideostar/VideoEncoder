//go:build !windows

package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type sysMonitor struct {
	lastIdle, lastTotal uint64
	lastProc            map[int]uint64
	lastAt              time.Time
}

func newSysMonitor() *sysMonitor { return &sysMonitor{lastProc: map[int]uint64{}} }

func (m *sysMonitor) sample(pids []int) SysStat {
	st := SysStat{Procs: map[int]ProcStat{}}
	now := time.Now()
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
		var total, idle uint64
		for i, v := range f[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			total += n
			if i == 3 || i == 4 {
				idle += n
			}
		}
		if m.lastTotal > 0 && total > m.lastTotal {
			st.CPU = clampPct((1 - float64(idle-m.lastIdle)/float64(total-m.lastTotal)) * 100)
		}
		m.lastIdle, m.lastTotal = idle, total
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var tot, avail uint64
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				n, _ := strconv.ParseUint(f[1], 10, 64)
				switch f[0] {
				case "MemTotal:":
					tot = n * 1024
				case "MemAvailable:":
					avail = n * 1024
				}
			}
		}
		st.MemTotal, st.MemUsed = tot, tot-avail
	}
	elapsed := now.Sub(m.lastAt).Seconds()
	cores := float64(runtimeCores())
	newProc := map[int]uint64{}
	for _, pid := range pids {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		if i := strings.LastIndexByte(s, ')'); i > 0 {
			s = s[i+2:]
		}
		f := strings.Fields(s)
		if len(f) < 22 {
			continue
		}
		ut, _ := strconv.ParseUint(f[11], 10, 64)
		stt, _ := strconv.ParseUint(f[12], 10, 64)
		rss, _ := strconv.ParseUint(f[21], 10, 64)
		t := ut + stt // ticks (100/s)
		newProc[pid] = t
		ps := ProcStat{Mem: rss * 4096}
		if prev, ok := m.lastProc[pid]; ok && elapsed > 0 && t >= prev {
			ps.CPU = clampPct(float64(t-prev) / 100 / elapsed / cores * 100)
		}
		st.Procs[pid] = ps
	}
	m.lastProc = newProc
	m.lastAt = now
	return st
}
