// Package jobs runs the background jobs (panel sync, traffic, backups…) on
// their tickers and on demand, and records each run: a status row per job
// and a bounded history of successes and failures.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"remna-control/ent"
)

var (
	ErrRunning = errors.New("job is already running")
	ErrUnknown = errors.New("unknown job")
)

// Result is what one run of a job reports.
type Result struct {
	Summary string // one line; shown in the UI, "" when nothing to say
	Changed bool   // the run changed something: a success history row is written
	Skipped bool   // the tick wasn't due: nothing is recorded at all
}

type Func func(ctx context.Context) (Result, error)

type Job struct {
	Key      string
	Every    time.Duration        // ticker period
	Schedule func() time.Duration // shown interval; nil = Every; 0 = schedule off ("disabled" in UI)
	Tick     Func                 // scheduled run (may return Skipped)
	Manual   Func                 // "Run now" and manual endpoints; nil = Tick
	Throttle bool                 // idle successes get a history row at most once per SuccessThrottle
}

// entry is a registered job with its run lock: a tick and a manual run of
// the same job never overlap.
type entry struct {
	Job
	mu      sync.Mutex
	running atomic.Bool
}

type Manager struct {
	db      *ent.Client
	enabled bool
	now     func() time.Time

	jobs  []*entry
	byKey map[string]*entry
}

// New makes a manager; with enabled=false (-no-sync) the loops never start
// and every job shows as disabled.
func New(db *ent.Client, enabled bool) *Manager {
	return &Manager{db: db, enabled: enabled, now: time.Now, byKey: map[string]*entry{}}
}

// Register adds a job; call it before Start. Jobs are listed in
// registration order.
func (m *Manager) Register(j Job) {
	if _, ok := m.byKey[j.Key]; ok {
		panic("jobs: duplicate key " + j.Key)
	}
	e := &entry{Job: j}
	m.jobs = append(m.jobs, e)
	m.byKey[j.Key] = e
}

// Start runs every job now and then every Every until ctx is done, one
// goroutine per job. It returns at once.
func (m *Manager) Start(ctx context.Context) {
	if !m.enabled {
		return
	}
	for _, e := range m.jobs {
		go m.loop(ctx, e)
	}
}

func (m *Manager) loop(ctx context.Context, e *entry) {
	t := time.NewTicker(e.Every)
	defer t.Stop()
	for {
		m.tick(ctx, e)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tick runs the scheduled func, unless the job is already running.
func (m *Manager) tick(ctx context.Context, e *entry) {
	_, _ = m.locked(ctx, e, e.Tick) // the run is recorded and logged
}

// Run runs the job's manual func now and returns its updated view. The
// error is ErrUnknown, ErrRunning, or the job's own error as it returned it
// (the run is recorded as failed then).
func (m *Manager) Run(ctx context.Context, key string) (View, error) {
	e, ok := m.byKey[key]
	if !ok {
		return View{}, ErrUnknown
	}
	fn := e.Manual
	if fn == nil {
		fn = e.Tick
	}
	ran, runErr := m.locked(ctx, e, fn)
	if !ran {
		return View{}, ErrRunning
	}
	v, err := m.Status(ctx, key)
	if runErr != nil {
		return v, runErr
	}
	return v, err
}

// locked runs fn under the job's lock and returns the job's error; ran is
// false if the job is already running.
func (m *Manager) locked(ctx context.Context, e *entry, fn Func) (ran bool, err error) {
	if !e.mu.TryLock() {
		return false, nil
	}
	defer e.mu.Unlock()
	e.running.Store(true)
	defer e.running.Store(false)
	_, err = m.exec(ctx, e, fn)
	return true, err
}

// exec runs fn once, records the outcome and returns the job's own result;
// recording problems are only logged.
func (m *Manager) exec(ctx context.Context, e *entry, fn Func) (Result, error) {
	start := m.now()
	res, err := call(ctx, e.Key, fn)
	end := m.now()
	switch {
	case err != nil && ctx.Err() != nil:
		return res, err // cancelled (shutdown): not the job's failure
	case err == nil && res.Skipped:
		return res, err
	case err != nil:
		slog.Error("job failed", "job", e.Key, "error", err)
	}
	if rerr := m.record(context.WithoutCancel(ctx), &e.Job, start, end, res, err); rerr != nil {
		slog.Warn("background job record failed", "job", e.Key, "error", rerr)
	}
	return res, err
}

// call runs fn, turning a panic into an error.
func call(ctx context.Context, key string, fn Func) (res Result, err error) {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("background job panicked", "job", key, "panic", v, "stack", string(debug.Stack()))
			res, err = Result{}, fmt.Errorf("panic: %v", v)
		}
	}()
	return fn(ctx)
}
