package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Job struct {
	ID       string         `json:"id"`
	Input    string         `json:"input"`
	Output   string         `json:"output"`
	Options  *EncodeOptions `json:"options"`
	Preset   string         `json:"preset"`
	Args     []string       `json:"args"`
	Cmd      string         `json:"cmd"`
	Status   string         `json:"status"` // queued running paused done error canceled
	Progress float64        `json:"progress"`
	FPS      float64        `json:"fps"`
	Speed    string         `json:"speed"`
	Bitrate  string         `json:"bitrate"`
	Size     int64          `json:"size"`
	OutTime  float64        `json:"out_time"`
	Duration float64        `json:"duration"`
	ETA      float64        `json:"eta"`
	Error    string         `json:"error,omitempty"`
	Created  time.Time      `json:"created"`
	Started  *time.Time     `json:"started,omitempty"`
	Finished *time.Time     `json:"finished,omitempty"`
	Log      []string       `json:"log,omitempty"`

	cmd      *exec.Cmd
	canceled bool
}

type JobManager struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	order   []string
	paused  bool
	wake    chan struct{}
	seq     int
	saveTmr *time.Timer
}

func jobsFile() string { return filepath.Join(baseDir, "jobs.json") }

func NewJobManager() *JobManager {
	m := &JobManager{jobs: map[string]*Job{}, wake: make(chan struct{}, 1)}
	if b, err := os.ReadFile(jobsFile()); err == nil {
		var list []*Job
		if json.Unmarshal(b, &list) == nil {
			for _, j := range list {
				if j.Status == "running" || j.Status == "paused" {
					j.Status = "error"
					j.Error = "întrerupt (aplicația a fost repornită)"
				}
				m.jobs[j.ID] = j
				m.order = append(m.order, j.ID)
			}
		}
	}
	go m.loop()
	return m
}

func (m *JobManager) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *JobManager) loop() {
	t := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-m.wake:
		case <-t.C:
		}
		m.schedule()
	}
}

func (m *JobManager) schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paused {
		return
	}
	running := 0
	for _, j := range m.jobs {
		if j.Status == "running" || j.Status == "paused" {
			running++
		}
	}
	max := getCfg().MaxJobs
	for _, id := range m.order {
		if running >= max {
			break
		}
		j := m.jobs[id]
		if j != nil && j.Status == "queued" {
			running++
			j.Status = "running"
			now := time.Now()
			j.Started = &now
			go m.run(j)
		}
	}
}

func (m *JobManager) Add(input string, o *EncodeOptions, preset string) *Job {
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("%s%03d", time.Now().Format("060102150405"), m.seq%1000)
	m.mu.Unlock()

	opts := *o
	out := OutputPath(input, &opts)
	m.mu.Lock()
	// evită două joburi cu aceeași ieșire în coadă
	for _, other := range m.jobs {
		if (other.Status == "queued" || other.Status == "running") && samePath(other.Output, out) && !opts.Overwrite {
			ext := filepath.Ext(out)
			out = strings.TrimSuffix(out, ext) + "_" + id[len(id)-4:] + ext
		}
	}
	args := BuildArgs(input, out, &opts)
	j := &Job{
		ID: id, Input: input, Output: out, Options: &opts, Preset: preset, Args: args,
		Cmd: CommandString(ffmpegExe(), args), Status: "queued", Created: time.Now(),
	}
	m.jobs[id] = j
	m.order = append(m.order, id)
	m.mu.Unlock()
	m.saveSoon()
	m.kick()
	return j
}

func (j *Job) addLog(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	j.Log = append(j.Log, line)
	if len(j.Log) > 400 {
		j.Log = j.Log[len(j.Log)-400:]
	}
}

func (m *JobManager) run(j *Job) {
	c := getCfg()
	exe := ffmpegExe()

	// durata (pentru progres)
	dur := probeDuration(j.Input)
	o := j.Options
	st, hasS := ParseTime(o.Start)
	en, hasE := ParseTime(o.End)
	if hasE && en > st {
		dur = en - st
	} else if hasS && dur > st {
		dur -= st
	}

	os.MkdirAll(filepath.Dir(j.Output), 0755)

	args := append([]string{}, j.Args...)
	// inserăm -progress înainte de ieșire
	args = append(args[:len(args)-1], "-progress", "pipe:1", "-nostats", args[len(args)-1])
	cmd := exec.Command(exe, args...)
	setProcAttrs(cmd, c.LowPriority)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	m.mu.Lock()
	j.Duration = dur
	j.cmd = cmd
	j.Log = nil
	j.addLog("> " + j.Cmd)
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
		m.finish(j, fmt.Errorf("nu pot porni ffmpeg (%s): %v", exe, err))
		return
	}
	log.Printf("job %s pornit: %s", j.ID, filepath.Base(j.Input))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.readProgress(j, stdout)
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(scanLinesCR)
		for sc.Scan() {
			m.mu.Lock()
			j.addLog(sc.Text())
			m.mu.Unlock()
		}
	}()
	wg.Wait()
	err := cmd.Wait()
	m.finish(j, err)
}

func scanLinesCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (m *JobManager) readProgress(j *Job, r io.Reader) {
	sc := bufio.NewScanner(r)
	start := time.Now()
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		m.mu.Lock()
		switch k {
		case "out_time_us", "out_time_ms":
			if us, err := strconv.ParseInt(v, 10, 64); err == nil && us >= 0 {
				j.OutTime = float64(us) / 1e6
			}
		case "fps":
			j.FPS, _ = strconv.ParseFloat(v, 64)
		case "speed":
			j.Speed = v
		case "bitrate":
			j.Bitrate = v
		case "total_size":
			j.Size, _ = strconv.ParseInt(v, 10, 64)
		case "progress":
			if j.Duration > 0 {
				p := j.OutTime / j.Duration * 100
				if p > 100 {
					p = 100
				}
				j.Progress = p
				sp, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(j.Speed), "x"), 64)
				if sp > 0 {
					j.ETA = (j.Duration - j.OutTime) / sp
				} else if el := time.Since(start).Seconds(); p > 0.5 {
					j.ETA = el/p*100 - el
				}
			}
		}
		m.mu.Unlock()
	}
}

func (m *JobManager) finish(j *Job, err error) {
	m.mu.Lock()
	now := time.Now()
	j.Finished = &now
	j.cmd = nil
	switch {
	case j.canceled:
		j.Status = "canceled"
	case err != nil:
		j.Status = "error"
		msg := err.Error()
		// ultima linie relevantă din log
		for i := len(j.Log) - 1; i >= 0; i-- {
			l := j.Log[i]
			if !strings.HasPrefix(l, "> ") && strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "Exiting") && !strings.HasPrefix(l, "Conversion failed") {
				msg = l
				break
			}
		}
		j.Error = msg
	default:
		j.Status = "done"
		j.Progress = 100
		j.ETA = 0
		if j.Options.Container == "hls" {
			var total int64
			ents, _ := os.ReadDir(filepath.Dir(j.Output))
			for _, e := range ents {
				if inf, err := e.Info(); err == nil && !inf.IsDir() {
					total += inf.Size()
				}
			}
			j.Size = total
		} else if st, e := os.Stat(j.Output); e == nil && !st.IsDir() {
			j.Size = st.Size()
		}
	}
	log.Printf("job %s: %s %s", j.ID, j.Status, j.Error)
	m.mu.Unlock()
	m.saveSoon()
	m.kick()
}

func (m *JobManager) Action(id, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	if j == nil {
		return fmt.Errorf("job inexistent")
	}
	switch action {
	case "cancel":
		if j.Status == "queued" {
			j.Status = "canceled"
		} else if (j.Status == "running" || j.Status == "paused") && j.cmd != nil && j.cmd.Process != nil {
			j.canceled = true
			if j.Status == "paused" {
				resumeProcess(j.cmd.Process)
			}
			j.cmd.Process.Kill()
		}
	case "pause":
		if j.Status != "running" || j.cmd == nil || j.cmd.Process == nil {
			return fmt.Errorf("jobul nu rulează")
		}
		if err := suspendProcess(j.cmd.Process); err != nil {
			return err
		}
		j.Status = "paused"
	case "resume":
		if j.Status != "paused" || j.cmd == nil {
			return fmt.Errorf("jobul nu e în pauză")
		}
		if err := resumeProcess(j.cmd.Process); err != nil {
			return err
		}
		j.Status = "running"
	case "retry":
		if j.Status == "running" || j.Status == "paused" {
			return fmt.Errorf("jobul rulează")
		}
		j.Status, j.Error, j.Progress, j.ETA, j.OutTime, j.Size = "queued", "", 0, 0, 0, 0
		j.canceled, j.Started, j.Finished = false, nil, nil
		// reconstruim cu overwrite, ieșirea parțială poate exista
		o := *j.Options
		o.Overwrite = true
		j.Args = BuildArgs(j.Input, j.Output, &o)
		j.Cmd = CommandString(ffmpegExe(), j.Args)
	case "up", "down", "top":
		idx := -1
		for i, x := range m.order {
			if x == id {
				idx = i
			}
		}
		if idx < 0 {
			break
		}
		m.order = append(m.order[:idx], m.order[idx+1:]...)
		n := idx
		if action == "up" && n > 0 {
			n--
		} else if action == "down" && n < len(m.order) {
			n++
		} else if action == "top" {
			n = 0
		}
		m.order = append(m.order[:n], append([]string{id}, m.order[n:]...)...)
	default:
		return fmt.Errorf("acțiune necunoscută")
	}
	go m.saveSoon()
	go m.kick()
	return nil
}

func (m *JobManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	if j == nil {
		return fmt.Errorf("job inexistent")
	}
	if j.Status == "running" || j.Status == "paused" {
		return fmt.Errorf("oprește întâi jobul")
	}
	delete(m.jobs, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	go m.saveSoon()
	return nil
}

func (m *JobManager) ClearFinished() {
	m.mu.Lock()
	var keep []string
	for _, id := range m.order {
		j := m.jobs[id]
		if j.Status == "done" || j.Status == "error" || j.Status == "canceled" {
			delete(m.jobs, id)
		} else {
			keep = append(keep, id)
		}
	}
	m.order = keep
	m.mu.Unlock()
	m.saveSoon()
}

func (m *JobManager) Get(id string, withLog bool) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	if j == nil {
		return nil
	}
	c := *j
	c.cmd = nil
	if withLog {
		c.Log = append([]string{}, j.Log...)
	} else {
		c.Log = nil
	}
	return &c
}

func (m *JobManager) List() ([]*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, 0, len(m.order))
	for _, id := range m.order {
		c := *m.jobs[id]
		c.cmd = nil
		c.Log = nil
		c.Args = nil
		out = append(out, &c)
	}
	return out, m.paused
}

func (m *JobManager) SetPaused(p bool) {
	m.mu.Lock()
	m.paused = p
	m.mu.Unlock()
	m.kick()
}

func (m *JobManager) saveSoon() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveTmr != nil {
		return
	}
	m.saveTmr = time.AfterFunc(time.Second, func() {
		m.mu.Lock()
		m.saveTmr = nil
		list := make([]Job, 0, len(m.order))
		for _, id := range m.order {
			c := *m.jobs[id]
			c.cmd = nil
			if len(c.Log) > 60 {
				c.Log = c.Log[len(c.Log)-60:]
			}
			list = append(list, c)
		}
		m.mu.Unlock()
		b, _ := json.MarshalIndent(list, "", " ")
		tmp := jobsFile() + ".tmp"
		if os.WriteFile(tmp, b, 0644) == nil {
			os.Rename(tmp, jobsFile())
		}
	})
}
