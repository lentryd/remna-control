package jobs

import (
	"context"
	"time"

	"remna-control/ent"
	"remna-control/ent/jobrun"
	"remna-control/ent/jobstatus"
)

// View is a job's state for the UI: its schedule plus its job_status row
// (zero values before the first recorded run).
type View struct {
	Key             string     `json:"key"`
	IntervalSeconds *int       `json:"interval_seconds"` // nil when the schedule is off
	Enabled         bool       `json:"enabled"`
	Running         bool       `json:"running"`
	LastRunAt       *time.Time `json:"last_run_at"`
	LastSuccessAt   *time.Time `json:"last_success_at"`
	LastFailureAt   *time.Time `json:"last_failure_at"`
	LastOK          *bool      `json:"last_ok"` // nil = never run
	LastDurationMS  int64      `json:"last_duration_ms"`
	LastSummary     string     `json:"last_summary"`
	LastError       string     `json:"last_error"`
	Runs            int        `json:"runs"`
	Failures        int        `json:"failures"`
}

// RunRow is one history entry; Repeat counts identical failures folded
// into it (FinishedAt is the last of them).
type RunRow struct {
	ID         int       `json:"id"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
	OK         bool      `json:"ok"`
	Text       string    `json:"text"`
	Repeat     int       `json:"repeat"`
}

// List returns every registered job, in registration order.
func (m *Manager) List(ctx context.Context) ([]View, error) {
	keys := make([]string, len(m.jobs))
	for i, e := range m.jobs {
		keys[i] = e.Key
	}
	rows, err := m.db.JobStatus.Query().Where(jobstatus.JobIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*ent.JobStatus, len(rows))
	for _, r := range rows {
		byKey[r.Job] = r
	}
	out := make([]View, len(m.jobs))
	for i, e := range m.jobs {
		out[i] = m.view(e, byKey[e.Key])
	}
	return out, nil
}

// Status returns one job's view.
func (m *Manager) Status(ctx context.Context, key string) (View, error) {
	e, ok := m.byKey[key]
	if !ok {
		return View{}, ErrUnknown
	}
	st, err := m.db.JobStatus.Query().Where(jobstatus.Job(key)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return View{}, err
	}
	return m.view(e, st), nil
}

// History returns the job's history rows, newest first; retention keeps
// the list short, so there is no paging.
func (m *Manager) History(ctx context.Context, key string, onlyFailures bool) ([]RunRow, error) {
	if _, ok := m.byKey[key]; !ok {
		return nil, ErrUnknown
	}
	q := m.db.JobRun.Query().Where(jobrun.Job(key))
	if onlyFailures {
		q.Where(jobrun.Ok(false))
	}
	rows, err := q.Order(ent.Desc(jobrun.FieldStartedAt), ent.Desc(jobrun.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RunRow, len(rows))
	for i, r := range rows {
		out[i] = RunRow{
			ID: r.ID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, DurationMS: r.DurationMs,
			OK: r.Ok, Text: r.Text, Repeat: r.Repeat,
		}
	}
	return out, nil
}

// view merges a job's schedule with its status row (nil before any run).
func (m *Manager) view(e *entry, st *ent.JobStatus) View {
	every := e.Every
	if e.Schedule != nil {
		every = e.Schedule()
	}
	v := View{Key: e.Key, Enabled: m.enabled && every != 0, Running: e.running.Load()}
	if every != 0 {
		sec := int(every / time.Second)
		v.IntervalSeconds = &sec
	}
	if st == nil {
		return v
	}
	ok := st.LastOk
	v.LastRunAt, v.LastSuccessAt, v.LastFailureAt = st.LastRunAt, st.LastSuccessAt, st.LastFailureAt
	v.LastOK, v.LastDurationMS = &ok, st.LastDurationMs
	v.LastSummary, v.LastError = st.LastSummary, st.LastError
	v.Runs, v.Failures = st.Runs, st.Failures
	return v
}
