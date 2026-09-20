package wakeup

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alex0esc/ceres/internal/task"
	"github.com/robfig/cron/v3"
)

// SubmitFunc submits a task to an agent and returns a channel that receives
// its result. It is meant to be the SubmitTask method of the agent (as a
// method value, e.g. ag.SubmitTask), so this package never imports the agent.
type SubmitFunc func(task.Task) <-chan task.TaskResult

// WakeUp wraps a single scheduled wakeup: either one-shot (fireAt set) or
// recurring (cronSpec set).
type WakeUp struct {
	name        string
	description string
	fireAt      time.Time
	cronSpec    string
	task        task.Task // list of prompts, worked through in order

	mu      sync.Mutex
	cron    *cron.Cron   // set once Start has registered the wakeup
	timer   *time.Timer  // set for one-shot wakeups
	cronID  cron.EntryID // set for recurring wakeups
	started bool
}

func NewWakeUp(
	name string,
	description string,
	fireAt time.Time,
	cronSpec string,
	task task.Task,
) *WakeUp {
	return &WakeUp{
		name:        name,
		description: description,
		fireAt:      fireAt,
		cronSpec:    cronSpec,
		task:        task,
	}
}

func (w *WakeUp) Name() string        { return w.name }
func (w *WakeUp) Description() string { return w.description }
func (w *WakeUp) FireAt() time.Time   { return w.fireAt }
func (w *WakeUp) CronSpec() string    { return w.cronSpec }
func (w *WakeUp) Task() task.Task     { return w.task }

// Running reports whether the wakeup is currently scheduled (Start was
// called and it hasn't fired/been stopped since).
func (w *WakeUp) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.started
}

// executes the wakeup asynchronously. Needs Start to have been called, that is
// where the wakeup gets its submit function.
func (w *WakeUp) Execute(submit SubmitFunc) {
	if submit == nil {
		slog.Error(fmt.Sprintf("cannot run wakeup %s: it was never started", w.name))
		return
	}

	go func() {
		res := <-submit(w.task)
		if res.Err != nil {
			slog.Error(fmt.Sprintf("error while running wakeup %s: %v", w.name, res.Err))
		}
	}()
}

// Start schedules the wakeup. submit is the function the wakeup uses to hand
// its task to the agent. Recurring wakeups register against the shared
// *cron.Cron. One-shot wakeups schedule a timer. Keeps a handle to whatever it
// registered so Stop can cancel it later.
// fireAt wakeups that are overdue fire instantly at startup (to catch up).
// afterFunc is called after a one-shot wakeup has executed (use for cleanup,
// e.g. removing it from the database).
func (w *WakeUp) Start(c *cron.Cron, submit SubmitFunc, afterFunc func()) error {
	if submit == nil {
		return fmt.Errorf("wakeup %q: no submit function", w.name)
	}

	w.mu.Lock()
	defer w.mu.Unlock()


	if w.cronSpec != "" {
		id, err := c.AddFunc(w.cronSpec, func() { w.Execute(submit) })
		if err != nil {
			return fmt.Errorf("wakeup %q: invalid schedule %q: %w", w.name, w.cronSpec, err)
		}
		w.cron = c
		w.cronID = id
		w.started = true
		return nil
	}

	if w.fireAt.IsZero() {
		return fmt.Errorf("wakeup %q: neither fire_at nor cron_spec is set", w.name)
	}

	delay := max(time.Until(w.fireAt), 0)

	// Wrapper für One-Shots: Execute + nachgelagerte Cleanup-Funktion
	w.timer = time.AfterFunc(delay, func() {
		w.Execute(submit)
		if afterFunc != nil {
			afterFunc()
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
