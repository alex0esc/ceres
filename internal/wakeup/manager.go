package wakeup

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// ErrProtected is returned when trying to remove a wakeup that was not created
// by the agent (see Manager.Add).
var ErrExternal = errors.New("wakeup: entry is protected")

// Manager owns the wakeups of one agent. It is the only place that touches
// them, so all locking lives here. The wakeups are kept in two separate lists:
//   - prompt wakeups are created by the agent itself (AddPromptWakeup), are
//     stored in the wakeup config (database) and can be removed again
//     (RemovePromptWakeup).
//   - external wakeups are added from outside (Add), e.g. from the user's
//     config file. They are not stored by the manager and can never be
//     removed by the agent.
// Names are unique across both lists. The manager is given the agent's submit
// function once (NewManager) and hands it to every wakeup when it is started.
type Manager struct {
	mu        sync.Mutex
	agentName string
	submit    SubmitFunc
	cron      *cron.Cron
	running   bool // StartAll was called and StopAll was not

	internal  map[string]*WakeUp
	external map[string]*WakeUp
}

// NewManager loads the prompt wakeups of agentName from the database (DbOpen
// must have been called). submit is the agent's submit function (its
// SubmitTask), cronLib schedules recurring wakeups. Nothing is scheduled until
// StartAll.
func NewManager(agentName string, submit SubmitFunc, cronLib *cron.Cron) (*Manager, error) {
	if submit == nil {
		return nil, fmt.Errorf("wakeup manager for agent %q: submit function is nil", agentName)
	}

	internal, err := DbLoadWakeups(agentName)
	if err != nil {
		return nil, fmt.Errorf("wakeup manager for agent %q: %w", agentName, err)
	}

	return &Manager{
		agentName: agentName,
		submit:    submit,
		cron:      cronLib,
		internal:   internal,
		external:  make(map[string]*WakeUp),
	}, nil
}

// StartAll schedules all wakeups. From now on wakeups that are added are
// scheduled right away. A wakeup that cannot be scheduled is logged and
// skipped. Does nothing if the manager is already running.
func (m *Manager) StartAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}
	m.running = true

	for _, group := range []map[string]*WakeUp{m.internal, m.external} {
		for _, wu := range group {
			if err := m.start(wu); err != nil {
				slog.Error(fmt.Sprintf("agent %s: error starting wakeup %s: %v", m.agentName, wu.Name(), err))
			}
		}
	}
}

// StopAll cancels the schedule of all wakeups (nothing is deleted). Wakeups
// added while stopped are scheduled by the next StartAll.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.running = false

	for _, group := range []map[string]*WakeUp{m.internal, m.external} {
		for _, wu := range group {
			wu.Stop()
		}
	}
}

// Get returns the wakeup with the given name, whichever list it is in.
func (m *Manager) Get(name string) (*WakeUp, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if wu, ok := m.internal[name]; ok {
		return wu, true
	}
	wu, ok := m.external[name]
	return wu, ok
}

// Run executes the wakeup with the given name once, right now and
// asynchronously, regardless of its schedule. Returns false if there is no
// such wakeup.
func (m *Manager) Run(name string) bool {
	wu, ok := m.Get(name)
	if !ok {
		return false
	}
	wu.Execute(m.submit)
	return true
}

// ListInternalWakeups returns all wakeups created by the agent itself,
// sorted by name. Internal wakeups can be edited or removed by the agent.
func (m *Manager) ListInternalWakeups() []*WakeUp {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*WakeUp, 0, len(m.internal))
	for _, wu := range m.internal {
		out = append(out, wu)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})

	return out
}

// ListExternalWakeups returns all wakeups created externally, e.g. by the
// user's configuration, sorted by name. External wakeups are protected and
// cannot be edited or removed by the agent.
func (m *Manager) ListExternalWakeups() []*WakeUp {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*WakeUp, 0, len(m.external))
	for _, wu := range m.external {
		out = append(out, wu)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})

	return out
}


// AddPromptWakeup creates a wakeup that runs prompt, writes it to the wakeup
// config (database) and adds it to the prompt wakeups. Set exactly one of
// fireAt/cronSpec. timeout is the timeout of the wakeup's task (at least one
// second); it is stored with the wakeup and used again after a restart.
func (m *Manager) AddPromptWakeup(
	name string,
	description string,
	fireAt *time.Time,
	cronSpec string,
	prompt string,
	timeout time.Duration,
	clearHistory bool,
) (*WakeUp, error) {
	// Everything that can be checked without touching state comes first.
	if cronSpec != "" {
		if _, err := cron.ParseStandard(cronSpec); err != nil {
			return nil, fmt.Errorf("wakeup %q: invalid cron spec %q: %w", name, cronSpec, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.existsLocked(name) {
		return nil, fmt.Errorf("wakeup %q: %w", name, ErrExists)
	}

	// Persist first, so a failed write never leaves a wakeup that is gone after a restart.
	// This also validates the fireAt/cronSpec combination, the prompt and the timeout.
	if err := DbAddWakeup(m.agentName, name, description, fireAt, cronSpec, prompt, timeout, clearHistory); err != nil {
		return nil, err
	}

	var fireAtTime time.Time
	if fireAt != nil {
		fireAtTime = *fireAt
	}

	wu := newPromptWakeUp(name, description, fireAtTime, cronSpec, prompt, timeout, clearHistory)
	if m.running {
		if err := m.start(wu); err != nil {
			_ = DbRemoveWakeup(m.agentName, name)
			return nil, err
		}
	}

	m.internal[name] = wu
	return wu, nil
}


// RemovePromptWakeup deletes a prompt wakeup from the database and stops it.
// External wakeups cannot be removed (ErrProtected), unknown names give
// ErrNotFound.
func (m *Manager) RemovePromptWakeup(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wu, ok := m.internal[name]
	if !ok {
		if _, isExternal := m.external[name]; isExternal {
			return fmt.Errorf("wakeup %q: %w", name, ErrExternal)
		}
		return fmt.Errorf("wakeup %q: %w", name, ErrNotFound)
	}

	// Database first: if it fails, nothing has changed yet. A row that is
	// already gone is fine, the wakeup is dropped from the list anyway.
	if err := DbRemoveWakeup(m.agentName, name); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	wu.Stop()
	delete(m.internal, name)
	return nil
}


// AddAll registers multiple wakeups that did not come from the wakeup config,
// e.g. ones from the user's config file. They are not stored by the manager
// and can never be removed by the agent (RemovePromptWakeup gives ErrProtected).
// They are scheduled right away if the manager is running, otherwise by StartAll.
// Names are unique across both lists (ErrExists). If any wakeup fails to add,
// the operation stops and returns the error (already added wakeups remain).
func (m *Manager) AddAll(wus []*WakeUp) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, wu := range wus {
		if wu == nil {
			return errors.New("wakeup: cannot add a nil wakeup")
		}

		if m.existsLocked(wu.Name()) {
			return fmt.Errorf("wakeup %q: %w", wu.Name(), ErrExists)
		}

		if m.running {
			if err := m.start(wu); err != nil {
				return err
			}
		}

		m.external[wu.Name()] = wu
	}
	
	return nil
}




// existsLocked reports whether name is taken in either list. The caller holds m.mu.
func (m *Manager) existsLocked(name string) bool {
	_, ininternal := m.internal[name]
	_, inExternal := m.external[name]
	return ininternal || inExternal
}

// start schedules wu and hands it the agent's submit function. The caller
// holds m.mu (the one-shot callback runs in another goroutine and simply waits
// for it).
func (m *Manager) start(wu *WakeUp) error {
	return wu.Start(m.cron, m.submit, func() { m.fired(wu) })
}

// fired cleans up a one-shot wakeup after it has run: it is dropped from its
// list, prompt wakeups are also deleted from the database. It only acts if
// the name still belongs to this very wakeup (it may have been removed or
// replaced in the meantime).
func (m *Manager) fired(wu *WakeUp) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := wu.Name()

	if m.internal[name] == wu {
		if err := DbRemoveWakeup(m.agentName, name); err != nil && !errors.Is(err, ErrNotFound) {
			slog.Error(fmt.Sprintf("agent %s: error removing fired wakeup %s: %v", m.agentName, name, err))
		}
		delete(m.internal, name)
		return
	}
	if m.external[name] == wu {
		delete(m.external, name)
	}
}
