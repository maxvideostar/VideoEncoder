package main

import (
	"runtime"
	"sync"
	"time"
)

// ProcStat – încărcarea unui proces ffmpeg (procente din tot calculatorul, ca în Task Manager)
type ProcStat struct {
	CPU float64 `json:"cpu"`
	GPU float64 `json:"gpu"`
	Mem uint64  `json:"mem"`
}

// SysStat – încărcarea sistemului
type SysStat struct {
	CPU      float64            `json:"cpu"`
	MemUsed  uint64             `json:"mem_used"`
	MemTotal uint64             `json:"mem_total"`
	GPU      float64            `json:"gpu"`     // cel mai încărcat motor GPU
	GPUEng   map[string]float64 `json:"gpu_eng"` // 3D, VideoDecode, VideoProcessing, VideoEncode…
	HasGPU   bool               `json:"has_gpu"`
	Cores    int                `json:"cores"`
	Procs    map[int]ProcStat   `json:"procs"`
	History  []float64          `json:"history"` // CPU ultimele ~2 minute
	Updated  time.Time          `json:"updated"`
}

var (
	sysMu   sync.Mutex
	sysLast SysStat
	sysHist []float64
)

func sysSnapshot() SysStat {
	sysMu.Lock()
	defer sysMu.Unlock()
	s := sysLast
	s.History = append([]float64{}, sysHist...)
	return s
}

func runningPIDs() []int {
	if jobs == nil {
		return nil
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	var pids []int
	for _, j := range jobs.jobs {
		if j.PID > 0 {
			pids = append(pids, j.PID)
		}
	}
	return pids
}

func sysMonitorLoop() {
	mon := newSysMonitor()
	for {
		st := mon.sample(runningPIDs())
		st.Cores = runtime.NumCPU()
		st.Updated = time.Now()
		sysMu.Lock()
		sysLast = st
		sysHist = append(sysHist, st.CPU)
		if len(sysHist) > 60 {
			sysHist = sysHist[len(sysHist)-60:]
		}
		sysMu.Unlock()
		time.Sleep(2 * time.Second)
	}
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func runtimeCores() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return n
}
