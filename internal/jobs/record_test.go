package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"remna-control/ent"
)

// register adds job "j" running s for both tick and manual runs.
func register(m *Manager, s *stub, throttle bool) {
	m.Register(Job{Key: "j", Every: time.Hour, Tick: s.fn, Throttle: throttle})
}

// mustRun runs job "j" now; the job's own failure is part of the view.
func mustRun(t *testing.T, m *Manager) View {
	t.Helper()
	v, err := m.Run(context.Background(), "j")
	if errors.Is(err, ErrRunning) || errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	return v
}

func TestFailureDedup(t *testing.T) {
	m, db := newTestManager(t, true)
	c := withClock(m)
	s := &stub{err: errors.New("boom")}
	register(m, s, false)
	for range 3 {
		mustRun(t, m)
		c.advance(time.Minute)
	}
	rows := runRows(t, db, "j", false)
	if len(rows) != 1 {
		t.Fatalf("failure rows = %d, want 1", len(rows))
	}
	if rows[0].Repeat != 3 || rows[0].Text != "boom" {
		t.Errorf("row = %+v, want repeat=3 text=boom", rows[0])
	}
	if !rows[0].FinishedAt.After(rows[0].StartedAt) {
		t.Errorf("finished_at %v not moved past started_at %v", rows[0].FinishedAt, rows[0].StartedAt)
	}
	st := statusRow(t, db, "j")
	if st.Runs != 3 || st.Failures != 3 || st.LastOk || st.LastError != "boom" {
		t.Errorf("status = %+v", st)
	}
}

func TestFailureAfterIdleSuccessIsNewRow(t *testing.T) {
	m, db := newTestManager(t, true)
	c := withClock(m)
	s := &stub{err: errors.New("boom")}
	register(m, s, false)
	mustRun(t, m)
	c.advance(time.Minute)
	s.err = nil
	mustRun(t, m) // recovery: writes a success row
	c.advance(time.Minute)
	mustRun(t, m) // idle success: no row
	c.advance(time.Minute)
	s.err = errors.New("boom")
	mustRun(t, m)

	if rows := runRows(t, db, "j", false); len(rows) != 2 || rows[0].Repeat != 1 || rows[1].Repeat != 1 {
		t.Fatalf("failure rows = %+v, want 2 rows with repeat=1", rows)
	}
	if st := statusRow(t, db, "j"); st.Failures != 2 || st.Runs != 4 || st.LastSuccessAt == nil {
		t.Errorf("status = %+v", st)
	}
}

func TestDifferentErrorsKeepOwnRows(t *testing.T) {
	m, db := newTestManager(t, true)
	c := withClock(m)
	s := &stub{}
	register(m, s, false)
	for _, msg := range []string{"a", "b", "b", "a"} {
		s.err = errors.New(msg)
		mustRun(t, m)
		c.advance(time.Minute)
	}
	rows := runRows(t, db, "j", false)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s×%d", r.Text, r.Repeat))
	}
	if want := "a×1 b×2 a×1"; strings.Join(got, " ") != want {
		t.Errorf("failure rows (newest first) = %v, want %s", got, want)
	}
}

// seedRows inserts n rows for job "j" started at base, base-1m, base-2m…
// in UTC, as the recorder stores them.
func seedRows(t *testing.T, db *ent.Client, ok bool, n int, base time.Time, text string) {
	t.Helper()
	for i := range n {
		at := base.Add(-time.Duration(i) * time.Minute).UTC()
		db.JobRun.Create().SetJob("j").SetOk(ok).SetText(fmt.Sprintf("%s %d", text, i)).
			SetStartedAt(at).SetFinishedAt(at).ExecX(context.Background())
	}
}

func TestFailureRetentionByCountAndAge(t *testing.T) {
	m, db := newTestManager(t, true)
	c := withClock(m)
	now := c.now()
	seedRows(t, db, false, FailureKeep+5, now.Add(-time.Hour), "recent")
	seedRows(t, db, false, 3, now.Add(-FailureMaxAge-time.Hour), "aged")
	seedRows(t, db, true, 5, now.Add(-time.Hour), "success")
	aged := now.Add(-FailureMaxAge - time.Hour).UTC()
	db.JobRun.Create().SetJob("other").SetOk(false).SetText("x").SetStartedAt(aged).SetFinishedAt(aged).
		ExecX(context.Background())

	register(m, &stub{err: errors.New("new")}, false)
	mustRun(t, m)

	rows := runRows(t, db, "j", false)
	if len(rows) != FailureKeep {
		t.Fatalf("failure rows = %d, want %d", len(rows), FailureKeep)
	}
	if rows[0].Text != "new" {
		t.Errorf("newest row = %q, want new", rows[0].Text)
	}
	for _, r := range rows {
		if strings.HasPrefix(r.Text, "aged") {
			t.Errorf("aged row kept: %q", r.Text)
		}
	}
	if want := fmt.Sprintf("recent %d", FailureKeep-2); rows[len(rows)-1].Text != want {
		t.Errorf("oldest kept = %q, want %q", rows[len(rows)-1].Text, want)
	}
	if n := len(runRows(t, db, "j", true)); n != 5 {
		t.Errorf("success rows = %d, want 5 (untouched)", n)
	}
	if n := len(runRows(t, db, "other", false)); n != 1 {
		t.Errorf("other job's rows = %d, want 1 (untouched)", n)
	}
}

func TestSuccessRowRules(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		m, db := newTestManager(t, true)
		withClock(m)
		register(m, &stub{res: Result{Summary: "nothing"}}, false)
		mustRun(t, m)
		mustRun(t, m)
		if n := len(runRows(t, db, "j", true)); n != 0 {
			t.Errorf("idle success rows = %d, want 0", n)
		}
		if st := statusRow(t, db, "j"); st.Runs != 2 || !st.LastOk || st.LastSummary != "nothing" || st.LastSuccessAt == nil {
			t.Errorf("status = %+v", st)
		}
	})
	t.Run("changed", func(t *testing.T) {
		m, db := newTestManager(t, true)
		c := withClock(m)
		register(m, &stub{res: Result{Summary: "synced 3", Changed: true}}, false)
		mustRun(t, m)
		c.advance(time.Minute)
		mustRun(t, m)
		rows := runRows(t, db, "j", true)
		if len(rows) != 2 || rows[0].Text != "synced 3" || rows[0].Repeat != 1 {
			t.Errorf("changed success rows = %+v, want 2", rows)
		}
	})
	t.Run("recovery", func(t *testing.T) {
		m, db := newTestManager(t, true)
		c := withClock(m)
		s := &stub{err: errors.New("boom")}
		register(m, s, false)
		mustRun(t, m)
		c.advance(time.Minute)
		s.err = nil
		mustRun(t, m)
		c.advance(time.Minute)
		mustRun(t, m)
		if n := len(runRows(t, db, "j", true)); n != 1 {
			t.Errorf("success rows = %d, want 1 (recovery only)", n)
		}
	})
	t.Run("throttle", func(t *testing.T) {
		m, db := newTestManager(t, true)
		c := withClock(m)
		register(m, &stub{res: Result{Summary: "12 nodes"}}, true)
		mustRun(t, m) // first: row
		c.advance(30 * time.Minute)
		mustRun(t, m) // within the hour: none
		c.advance(31 * time.Minute)
		mustRun(t, m) // an hour since the last row: row
		c.advance(time.Minute)
		mustRun(t, m) // none
		if n := len(runRows(t, db, "j", true)); n != 2 {
			t.Errorf("throttled success rows = %d, want 2", n)
		}
	})
}

func TestSuccessRetention(t *testing.T) {
	m, db := newTestManager(t, true)
	c := withClock(m)
	now := c.now()
	seedRows(t, db, true, SuccessKeep+5, now.Add(-time.Hour), "recent")
	seedRows(t, db, true, 3, now.Add(-SuccessMaxAge-time.Hour), "aged")
	seedRows(t, db, false, 5, now.Add(-SuccessMaxAge-time.Hour), "failure")

	register(m, &stub{res: Result{Summary: "new", Changed: true}}, false)
	mustRun(t, m)

	rows := runRows(t, db, "j", true)
	if len(rows) != SuccessKeep {
		t.Fatalf("success rows = %d, want %d", len(rows), SuccessKeep)
	}
	if rows[0].Text != "new" {
		t.Errorf("newest row = %q, want new", rows[0].Text)
	}
	for _, r := range rows {
		if strings.HasPrefix(r.Text, "aged") {
			t.Errorf("aged row kept: %q", r.Text)
		}
	}
	if n := len(runRows(t, db, "j", false)); n != 5 {
		t.Errorf("failure rows = %d, want 5 (untouched)", n)
	}
}

// TestRetentionByAge keeps fewer rows than the count limit, so only the age
// rule can drop the aged ones.
func TestRetentionByAge(t *testing.T) {
	cases := []struct {
		name   string
		ok     bool
		maxAge time.Duration
		job    *stub
	}{
		{"failure", false, FailureMaxAge, &stub{err: errors.New("new")}},
		{"success", true, SuccessMaxAge, &stub{res: Result{Summary: "new", Changed: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, db := newTestManager(t, true)
			c := withClock(m)
			aged := c.now().Add(-tc.maxAge - time.Hour)
			seedRows(t, db, tc.ok, 3, c.now().Add(-time.Hour), "recent")
			seedRows(t, db, tc.ok, 3, aged, "aged")
			seedRows(t, db, !tc.ok, 2, aged, "other kind")
			db.JobRun.Create().SetJob("other").SetOk(tc.ok).SetText("x").
				SetStartedAt(aged.UTC()).SetFinishedAt(aged.UTC()).ExecX(context.Background())

			register(m, tc.job, false)
			mustRun(t, m)

			rows := runRows(t, db, "j", tc.ok)
			var got []string
			for _, r := range rows {
				got = append(got, r.Text)
			}
			if want := "new|recent 0|recent 1|recent 2"; strings.Join(got, "|") != want {
				t.Errorf("rows = %v, want %s", got, want)
			}
			if n := len(runRows(t, db, "j", !tc.ok)); n != 2 {
				t.Errorf("other kind's rows = %d, want 2 (untouched)", n)
			}
			if n := len(runRows(t, db, "other", tc.ok)); n != 1 {
				t.Errorf("other job's rows = %d, want 1 (untouched)", n)
			}
		})
	}
}

func TestSkippedRecordsNothing(t *testing.T) {
	m, db := newTestManager(t, true)
	s := &stub{res: Result{Skipped: true, Changed: true, Summary: "x"}}
	register(m, s, true)
	v := mustRun(t, m)
	if s.calls.Load() != 1 {
		t.Fatalf("job calls = %d, want 1", s.calls.Load())
	}
	if v.Key != "j" || v.LastOK != nil || v.Runs != 0 {
		t.Errorf("view = %+v, want a never-run view", v)
	}
	if st := statusRow(t, db, "j"); st != nil {
		t.Errorf("status row written: %+v", st)
	}
	if n := db.JobRun.Query().CountX(context.Background()); n != 0 {
		t.Errorf("history rows = %d, want 0", n)
	}
}

func TestPartialFailureText(t *testing.T) {
	m, db := newTestManager(t, true)
	register(m, &stub{res: Result{Summary: "2 resets"}, err: errors.New("boom")}, false)
	v := mustRun(t, m)
	rows := runRows(t, db, "j", false)
	if len(rows) != 1 || rows[0].Text != "done: 2 resets; failed: boom" {
		t.Fatalf("failure rows = %+v", rows)
	}
	if v.LastSummary != "2 resets" || v.LastError != "done: 2 resets; failed: boom" || v.LastOK == nil || *v.LastOK {
		t.Errorf("view = %+v", v)
	}
}

func TestTruncatesHugeError(t *testing.T) {
	m, db := newTestManager(t, true)
	register(m, &stub{err: errors.New(strings.Repeat("ж", MaxTextBytes))}, false)
	mustRun(t, m)
	rows := runRows(t, db, "j", false)
	if len(rows) != 1 {
		t.Fatalf("failure rows = %d", len(rows))
	}
	text := rows[0].Text
	if len(text) > MaxTextBytes || !strings.HasSuffix(text, "… (truncated)") || !strings.HasPrefix(text, "жж") {
		t.Errorf("text: %d bytes, suffix %q", len(text), text[max(0, len(text)-20):])
	}
	if !strings.HasSuffix(strings.TrimSuffix(text, "… (truncated)"), "ж") {
		t.Error("cut inside a UTF-8 character")
	}
	if st := statusRow(t, db, "j"); st.LastError != text {
		t.Error("status last_error differs from the history text")
	}
}

func TestCancelledCtxNotRecorded(t *testing.T) {
	m, db := newTestManager(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &stub{}
	m.Register(Job{Key: "j", Every: time.Hour, Tick: func(ctx context.Context) (Result, error) {
		s.calls.Add(1)
		return Result{}, ctx.Err()
	}})
	m.tick(ctx, m.byKey["j"])
	if s.calls.Load() != 1 {
		t.Fatalf("job calls = %d, want 1", s.calls.Load())
	}
	if st := statusRow(t, db, "j"); st != nil {
		t.Errorf("status row written: %+v", st)
	}
	if n := db.JobRun.Query().CountX(context.Background()); n != 0 {
		t.Errorf("history rows = %d, want 0", n)
	}
}

func TestPanicBecomesFailure(t *testing.T) {
	quietLog(t)
	m, db := newTestManager(t, true)
	m.Register(Job{Key: "j", Every: time.Hour, Tick: func(context.Context) (Result, error) { panic("kaboom") }})
	v := mustRun(t, m)
	if v.LastOK == nil || *v.LastOK || v.Failures != 1 || !strings.Contains(v.LastError, "kaboom") {
		t.Errorf("view = %+v", v)
	}
	if rows := runRows(t, db, "j", false); len(rows) != 1 || !strings.Contains(rows[0].Text, "kaboom") {
		t.Errorf("failure rows = %+v", rows)
	}
	// The job's lock is released after a panic.
	if _, err := m.Run(context.Background(), "j"); err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Errorf("run after panic: %v, want the panic as the error", err)
	}
}

func TestRecordingErrorDoesNotFailJob(t *testing.T) {
	quietLog(t)
	ctx := context.Background()
	m, db := newTestManager(t, true)
	s := &stub{res: Result{Summary: "ok", Changed: true}, err: errors.New("boom")}
	register(m, s, false)
	_ = db.Close()

	res, err := m.exec(ctx, m.byKey["j"], s.fn)
	if res.Summary != "ok" || err == nil || err.Error() != "boom" {
		t.Errorf("exec = %+v, %v; want the job's own result", res, err)
	}
	_, _ = m.Run(ctx, "j") // the status read fails too; it must not panic
	m.tick(ctx, m.byKey["j"])
	if n := s.calls.Load(); n != 3 {
		t.Errorf("job calls = %d, want 3", n)
	}
	if v := m.byKey["j"]; v.running.Load() || !v.mu.TryLock() {
		t.Error("job left locked or running after a recording error")
	}
}
