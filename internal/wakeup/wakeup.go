package wakeup

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/robfig/cron/v3"
)

// WakeUp wraps a single scheduled wakeup: either one-shot (fireAt set) or
// recurring (cronSpec set).
type WakeUp struct {
	name        string
	description string
	agent       handles.AgentHandle
	fireAt      *time.Time
	cronSpec    string
	task        []string // list of prompts, worked through in order
	timeout     time.Duration
	protected   bool

	mu      sync.Mutex
	cron    *cron.Cron   // set once Start has registered the wakeup
	timer   *time.Timer  // set for one-shot wakeups
	cronID  cron.EntryID // set for recurring wakeups
	started bool
}

func NewWakeUp(
	name string,
	description string,
	ag handles.AgentHandle,
	fireAt *time.Time,
	cronSpec string,
	task []string,
	timeout time.Duration,
	protected bool,
) *WakeUp {
	return &WakeUp{
		name:        name,
		description: description,
		agent:       ag,
		fireAt:      fireAt,
		cronSpec:    cronSpec,
		task:        task,
		timeout:     timeout,
		protected:   protected,
	}
}

func (w *WakeUp) Name() string        { return w.name }
func (w *WakeUp) Description() string { return w.description }
func (w *WakeUp) FireAt() *time.Time  { return w.fireAt }
func (w *WakeUp) CroneSpec() string   { return w.cronSpec }
func (w *WakeUp) Protected() bool { return w.protected }


// Running reports whether the wakeup is currently scheduled (Start was
// called and it hasn't fired/been stopped since).
func (w *WakeUp) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.started
}

func (w *WakeUp) Execute() {
	prompts := make([]handles.Prompt, 0, len(w.task))
	for _, p := range w.task {
		prompts = append(prompts, handles.Prompt{Text: p})
	}
	task := handles.TaskClearAskMultiple(prompts, w.timeout)

	res := <-w.agent.SubmitTask(task)
	if res.Err != nil {
		slog.Error(fmt.Sprintf("error while running wakeup %s: %v", w.name, res.Err))
	}
}

// Start schedules the wakeup. Recurring wakeups register against the
// shared *cron.Cron. One-shot wakeups schedule a timer and remove
// themselves from disk once they've fired. Keeps a handle to whatever it
// registered so Stop can cancel it later.
func (w *WakeUp) Start(c *cron.Cron) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cronSpec != "" {
		id, err := c.AddFunc(w.cronSpec, w.Execute)
		if err != nil {
			return fmt.Errorf("wakeup %q: invalid schedule %q: %w", w.name, w.cronSpec, err)
		}
		w.cron = c
		w.cronID = id
		w.started = true
		return nil
	}

	if w.fireAt == nil {
		return fmt.Errorf("wakeup %q: neither fire_at nor cron_spec is set", w.name)
	}

	delay := max(time.Until(*w.fireAt), 0)
	w.timer = time.AfterFunc(delay, func() {
		w.Execute()
		if !w.agent.RemoveWakeup(w.name) {
			slog.Error(fmt.Sprintf("could not remove wakeup %s after it finished", w.name))
		}
	})
	w.started = true
	return nil
}

// Stop cancels whatever was registered by Start: stops the one-shot timer,
// or removes the recurring cron entry. Safe to call even if Start was
// never called, or if it already fired.
func (w *WakeUp) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return
	}

	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	if w.cron != nil {
		w.cron.Remove(w.cronID)
		w.cron = nil
	}
	w.started = false
}

// Save persists the wakeup's current fields to its agent's config file
// (upsert by name).
func (w *WakeUp) Save() error {
	entry := WakeupEntry{
		Name:        w.name,
		FireAt:      w.fireAt,
		CronSpec:    w.cronSpec,
		Description: w.description,
		Task:        w.task,
		Timeout:     w.timeout.String(),
		Protected:   w.protected,
	}
	return setWakeup(w.agent, entry)
}

// Delete stops the wakeup (if it's currently running) and removes it from
// its agent's config file.
func (w *WakeUp) Delete() error {
	w.Stop()
	return removeWakeup(w.agent, w.name)
}
