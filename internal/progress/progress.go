// Package progress tracks a job made of named steps (download models, separate, recognize,
// generate…) and reports its state to the UI: each step's progress, elapsed time and ETA, and a
// global progress weighted by how long each step usually takes.
package progress

import (
	"math"
	"sync"
	"time"
)

// Step is one step of the process, with its state and progress
type Step struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	State    string  `json:"state"` // pending, running, done, skipped, error
	Progress float64 `json:"progress"`
	Detail   string  `json:"detail"`
	Elapsed  float64 `json:"elapsed"` // s
	ETA      float64 `json:"eta"`     // estimated remaining s (-1 = unknown)
	started  time.Time
}

// Status is the full state sent to the UI
type Status struct {
	Steps    []Step  `json:"steps"`
	Current  int     `json:"current"`
	Progress float64 `json:"progress"` // global 0..1
	Device   string  `json:"device"`   // GPU/CPU used
	Elapsed  float64 `json:"elapsed"`
	Preview  string  `json:"preview,omitempty"` // optional URL of an intermediate result
}

// Tracker updates a Status and reports copies of it (at most every 150 ms, except at the start
// and end of a step). Weights: weight of each step ID in the global progress; skipped steps
// don't count.
type Tracker struct {
	mu      sync.Mutex
	st      Status
	t0      time.Time
	weights map[string]float64
	report  func(Status)
	last    time.Time
}

func NewTracker(steps []Step, weights map[string]float64, report func(Status)) *Tracker {
	for i := range steps {
		steps[i].State = "pending"
	}
	return &Tracker{st: Status{Steps: steps}, t0: time.Now(), weights: weights, report: report}
}

func (t *Tracker) idx(id string) int {
	for i, s := range t.st.Steps {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func (t *Tracker) emit(force bool) {
	if !force && time.Since(t.last) < 150*time.Millisecond {
		return
	}
	t.last = time.Now()
	var wsum, done float64
	for i := range t.st.Steps {
		s := &t.st.Steps[i]
		w := t.weights[s.ID]
		if s.State == "skipped" {
			continue
		}
		wsum += w
		switch s.State {
		case "done":
			done += w
		case "running":
			done += w * s.Progress
			s.Elapsed = time.Since(s.started).Seconds()
			s.ETA = -1
			if s.Progress > 0.02 && s.Elapsed > 1 {
				s.ETA = s.Elapsed * (1 - s.Progress) / s.Progress
			}
		}
	}
	if wsum > 0 {
		t.st.Progress = done / wsum
	}
	t.st.Elapsed = time.Since(t.t0).Seconds()
	cp := t.st
	cp.Steps = append([]Step(nil), t.st.Steps...)
	if t.report != nil {
		t.report(cp)
	}
}

func (t *Tracker) Start(id, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.idx(id)
	t.st.Current = i
	t.st.Steps[i].State, t.st.Steps[i].Detail, t.st.Steps[i].started, t.st.Steps[i].Progress = "running", detail, time.Now(), 0
	t.emit(true)
}

func (t *Tracker) Progress(id string, p float64, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.idx(id)
	t.st.Steps[i].Progress = math.Max(0, math.Min(1, p))
	if detail != "" {
		t.st.Steps[i].Detail = detail
	}
	t.emit(false)
}

func (t *Tracker) End(id, state, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.idx(id)
	s := &t.st.Steps[i]
	if !s.started.IsZero() {
		s.Elapsed = time.Since(s.started).Seconds()
	}
	s.State, s.ETA = state, 0
	if state == "done" {
		s.Progress = 1
	}
	if detail != "" {
		s.Detail = detail
	}
	t.emit(true)
}

// SetDevice and SetPreview change the status shown (reported with the next update)
func (t *Tracker) SetDevice(d string) {
	t.mu.Lock()
	t.st.Device = d
	t.mu.Unlock()
}

func (t *Tracker) SetPreview(url string) {
	t.mu.Lock()
	t.st.Preview = url
	t.emit(true)
	t.mu.Unlock()
}
