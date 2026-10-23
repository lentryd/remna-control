package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/jobrun"
	"remna-control/ent/jobstatus"
	"remna-control/internal/store"
)

func newTestManager(t *testing.T, enabled bool) (*Manager, *ent.Client) {
	t.Helper()
	db, _, err := store.OpenDB(context.Background(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, enabled), db
}

// quietLog silences slog for one test, so expected warnings don't clutter
// the output.
func quietLog(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// clock is a settable time source for Manager.now.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func withClock(m *Manager) *clock {
	c := &clock{t: time.Now()}
	m.now = c.now
	return c
}

// stub is a job function returning a set result and counting its calls.
type stub struct {
	res   Result
	err   error
	calls atomic.Int32
}

func (s *stub) fn(context.Context) (Result, error) {
	s.calls.Add(1)
	return s.res, s.err
}

func statusRow(t *testing.T, db *ent.Client, key string) *ent.JobStatus {
	t.Helper()
	st, err := db.JobStatus.Query().Where(jobstatus.Job(key)).Only(context.Background())
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func runRows(t *testing.T, db *ent.Client, key string, ok bool) []*ent.JobRun {
	t.Helper()
	rows, err := db.JobRun.Query().
		Where(jobrun.Job(key), jobrun.Ok(ok)).
		Order(ent.Desc(jobrun.FieldStartedAt), ent.Desc(jobrun.FieldID)).
		All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRunRejectsConcurrent(t *testing.T) {
	ctx := context.Background()
	m, db := newTestManager(t, true)
	started, release := make(chan struct{}), make(chan struct{})
	var ticks atomic.Int32
	m.Register(Job{
		Key:   "j",
		Every: time.Hour,
		Tick:  func(context.Context) (Result, error) { ticks.Add(1); return Result{Changed: true}, nil },
		Manual: func(context.Context) (Result, error) {
			close(started)
			<-release
			return Result{Summary: "done", Changed: true}, nil
		},
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := m.Run(ctx, "j"); err != nil {
			t.Errorf("first run: %v", err)
		}
	}()
	<-started

	if _, err := m.Run(ctx, "j"); !errors.Is(err, ErrRunning) {
		t.Fatalf("second run: got %v, want ErrRunning", err)
	}
	v, err := m.Status(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Running {
		t.Error("status of a running job: Running = false")
	}
	m.tick(ctx, m.byKey["j"])
	if n := ticks.Load(); n != 0 {
		t.Errorf("tick ran while the job was running (%d calls)", n)
	}

	close(release)
	wg.Wait()
	st := statusRow(t, db, "j")
	if st == nil || st.Runs != 1 {
		t.Fatalf("status = %+v, want runs=1", st)
	}
	if rows := runRows(t, db, "j", true); len(rows) != 1 {
		t.Errorf("success rows = %d, want 1", len(rows))
	}
	if v, _ := m.Status(ctx, "j"); v.Running {
		t.Error("finished job: Running = true")
	}
}

func TestRunUnknown(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t, true)
	if _, err := m.Run(ctx, "nope"); !errors.Is(err, ErrUnknown) {
		t.Errorf("Run: got %v, want ErrUnknown", err)
	}
	if _, err := m.Status(ctx, "nope"); !errors.Is(err, ErrUnknown) {
		t.Errorf("Status: got %v, want ErrUnknown", err)
	}
	if _, err := m.History(ctx, "nope", false); !errors.Is(err, ErrUnknown) {
		t.Errorf("History: got %v, want ErrUnknown", err)
	}
}

func TestStartLoopsRunImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, db := newTestManager(t, true)
	m.Register(Job{
		Key:   "j",
		Every: 10 * time.Millisecond,
		// Ticks after cancel fail with ctx.Err() and so record nothing,
		// which keeps the DB quiet once the test closes it.
		Tick: func(ctx context.Context) (Result, error) { return Result{}, ctx.Err() },
	})
	m.Start(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if st := statusRow(t, db, "j"); st != nil && st.Runs >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("loop didn't record two runs")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	// Wait out a run that may be recording right now.
	e := m.byKey["j"]
	e.mu.Lock()
	defer e.mu.Unlock()
}

func TestDisabledManagerDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, db := newTestManager(t, false)
	s := &stub{}
	m.Register(Job{Key: "j", Every: time.Millisecond, Tick: s.fn})
	m.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	if n := s.calls.Load(); n != 0 {
		t.Errorf("disabled manager ran the job %d times", n)
	}
	if st := statusRow(t, db, "j"); st != nil {
		t.Errorf("status row written: %+v", st)
	}
}

func TestViewDisabled(t *testing.T) {
	ctx := context.Background()
	s := &stub{}

	m, _ := newTestManager(t, false)
	m.Register(Job{Key: "j", Every: time.Minute, Tick: s.fn})
	v, err := m.Status(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if v.Enabled {
		t.Error("disabled manager: Enabled = true")
	}

	m, _ = newTestManager(t, true)
	m.Register(Job{Key: "off", Every: time.Minute, Schedule: func() time.Duration { return 0 }, Tick: s.fn})
	m.Register(Job{Key: "on", Every: time.Minute, Schedule: func() time.Duration { return 2 * time.Hour }, Tick: s.fn})
	m.Register(Job{Key: "every", Every: 5 * time.Minute, Tick: s.fn})
	list, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Key != "off" || list[1].Key != "on" || list[2].Key != "every" {
		t.Fatalf("List isn't in registration order: %+v", list)
	}
	if list[0].Enabled || list[0].IntervalSeconds != nil {
		t.Errorf("schedule off: Enabled=%v IntervalSeconds=%v", list[0].Enabled, list[0].IntervalSeconds)
	}
	if !list[1].Enabled || list[1].IntervalSeconds == nil || *list[1].IntervalSeconds != 7200 {
		t.Errorf("schedule 2h: %+v", list[1])
	}
	if !list[2].Enabled || list[2].IntervalSeconds == nil || *list[2].IntervalSeconds != 300 {
		t.Errorf("no schedule, every 5m: %+v", list[2])
	}
}

func TestManualUsesManualFunc(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t, true)
	tick := &stub{}
	manual := &stub{res: Result{Summary: "manual", Changed: true}}
	m.Register(Job{Key: "j", Every: time.Hour, Tick: tick.fn, Manual: manual.fn})
	v, err := m.Run(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if tick.calls.Load() != 0 || manual.calls.Load() != 1 {
		t.Errorf("tick calls %d, manual calls %d", tick.calls.Load(), manual.calls.Load())
	}
	if v.LastSummary != "manual" || v.LastOK == nil || !*v.LastOK || v.Runs != 1 {
		t.Errorf("view = %+v", v)
	}
}

func TestViewNeverRun(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t, true)
	m.Register(Job{Key: "j", Every: time.Minute, Tick: (&stub{}).fn})
	v, err := m.Status(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if v.Key != "j" || v.IntervalSeconds == nil || *v.IntervalSeconds != 60 || !v.Enabled || v.Running ||
		v.LastRunAt != nil || v.LastSuccessAt != nil || v.LastFailureAt != nil || v.LastOK != nil ||
		v.LastDurationMS != 0 || v.LastSummary != "" || v.LastError != "" || v.Runs != 0 || v.Failures != 0 {
		t.Errorf("view = %+v", v)
	}
	h, err := m.History(ctx, "j", false)
	if err != nil || len(h) != 0 {
		t.Errorf("history = %v, %v", h, err)
	}
}

// errBoom is a job's own error type, to check Run returns it as is.
type errBoom struct{ code int }

func (e *errBoom) Error() string { return "boom" }

func TestRunReturnsJobError(t *testing.T) {
	quietLog(t)
	ctx := context.Background()
	m, _ := newTestManager(t, true)
	sentinel := errors.New("sentinel")
	s := &stub{res: Result{Summary: "1 done"}, err: fmt.Errorf("pass: %w", &errBoom{code: 7})}
	m.Register(Job{Key: "j", Every: time.Hour, Tick: s.fn})
	m.Register(Job{Key: "k", Every: time.Hour, Tick: (&stub{err: sentinel}).fn})
	m.Register(Job{Key: "ok", Every: time.Hour, Tick: (&stub{res: Result{Summary: "fine"}}).fn})

	v, err := m.Run(ctx, "j")
	var be *errBoom
	if !errors.As(err, &be) || be.code != 7 || err != s.err {
		t.Fatalf("Run error = %v, want the job's own error", err)
	}
	if v.LastOK == nil || *v.LastOK || v.Failures != 1 || v.LastError != "done: 1 done; failed: pass: boom" {
		t.Errorf("view = %+v, want the failure recorded", v)
	}
	if _, err := m.Run(ctx, "k"); !errors.Is(err, sentinel) {
		t.Errorf("Run error = %v, want the sentinel", err)
	}
	v, err = m.Run(ctx, "ok")
	if err != nil || v.LastOK == nil || !*v.LastOK || v.LastSummary != "fine" {
		t.Errorf("successful Run = %+v, %v", v, err)
	}
}
