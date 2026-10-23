package jobs

import (
	"context"
	"time"
	"unicode/utf8"

	"remna-control/ent"
	"remna-control/ent/jobrun"
	"remna-control/ent/jobstatus"
	"remna-control/internal/store"
)

// History retention, per job. Failures are kept longer: they are what the
// owner looks for.
const (
	SuccessKeep     = 20
	SuccessMaxAge   = 48 * time.Hour
	FailureKeep     = 50
	FailureMaxAge   = 90 * 24 * time.Hour
	MaxTextBytes    = 8 << 10
	SuccessThrottle = time.Hour // idle successes of a Throttle job: one row per this
)

const truncMark = "… (truncated)"

// outcome is one finished run, as it is stored.
type outcome struct {
	key        string
	start, end time.Time
	dur        int64
	ok         bool
	changed    bool
	summary    string
	text       string // the history row's text: summary, or the failure
}

// record writes one run in a single transaction: the job_status upsert
// (the heartbeat), a history row when the rules call for one, and the prune
// of that job's rows of the same kind.
func (m *Manager) record(ctx context.Context, j *Job, start, end time.Time, res Result, runErr error) error {
	// Stored in UTC: SQLite compares these as text.
	o := outcome{
		key:     j.Key,
		start:   start.UTC(),
		end:     end.UTC(),
		dur:     end.Sub(start).Milliseconds(),
		ok:      runErr == nil,
		changed: res.Changed,
		summary: res.Summary,
		text:    res.Summary,
	}
	if !o.ok {
		o.text = failureText(res.Summary, runErr)
	}
	return store.WithTx(ctx, m.db, func(tx *ent.Tx) error {
		prev, err := tx.JobStatus.Query().Where(jobstatus.Job(o.key)).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		if err := upsertStatus(ctx, tx, o); err != nil {
			return err
		}
		if o.ok {
			return recordSuccess(ctx, tx, o, prev, j.Throttle)
		}
		return recordFailure(ctx, tx, o, prev)
	})
}

func upsertStatus(ctx context.Context, tx *ent.Tx, o outcome) error {
	q := tx.JobStatus.Create().
		SetJob(o.key).SetLastRunAt(o.start).SetLastOk(o.ok).SetLastDurationMs(o.dur).
		SetLastSummary(o.summary).SetRuns(1)
	if o.ok {
		q.SetLastSuccessAt(o.end)
	} else {
		q.SetLastFailureAt(o.end).SetLastError(o.text).SetFailures(1)
	}
	u := q.OnConflictColumns(jobstatus.FieldJob).
		UpdateLastRunAt().UpdateLastOk().UpdateLastDurationMs().UpdateLastSummary().AddRuns(1)
	if o.ok {
		u.UpdateLastSuccessAt()
	} else {
		u.UpdateLastFailureAt().UpdateLastError().AddFailures(1)
	}
	return u.Exec(ctx)
}

// recordSuccess writes a success row only if the run changed something,
// recovers from a failure, or (throttle) no success row was written within
// SuccessThrottle; idle runs leave just the heartbeat.
func recordSuccess(ctx context.Context, tx *ent.Tx, o outcome, prev *ent.JobStatus, throttle bool) error {
	write := o.changed || (prev != nil && !prev.LastOk)
	if !write && throttle {
		recent, err := tx.JobRun.Query().
			Where(jobrun.Job(o.key), jobrun.Ok(true), jobrun.StartedAtGT(o.start.Add(-SuccessThrottle))).
			Exist(ctx)
		if err != nil {
			return err
		}
		write = !recent
	}
	if !write {
		return nil
	}
	if err := insertRun(ctx, tx, o); err != nil {
		return err
	}
	return prune(ctx, tx, o.key, true, SuccessKeep, o.end.Add(-SuccessMaxAge))
}

// recordFailure folds a failure into the latest failure row when the text
// is the same and no success came in between; otherwise it adds a row.
func recordFailure(ctx context.Context, tx *ent.Tx, o outcome, prev *ent.JobStatus) error {
	last, err := tx.JobRun.Query().
		Where(jobrun.Job(o.key), jobrun.Ok(false)).
		Order(ent.Desc(jobrun.FieldStartedAt), ent.Desc(jobrun.FieldID)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if last != nil && last.Text == o.text &&
		(prev == nil || prev.LastSuccessAt == nil || !prev.LastSuccessAt.After(last.FinishedAt)) {
		err = tx.JobRun.UpdateOne(last).
			AddRepeat(1).SetFinishedAt(o.end).SetDurationMs(o.dur).
			Exec(ctx)
	} else {
		err = insertRun(ctx, tx, o)
	}
	if err != nil {
		return err
	}
	return prune(ctx, tx, o.key, false, FailureKeep, o.end.Add(-FailureMaxAge))
}

func insertRun(ctx context.Context, tx *ent.Tx, o outcome) error {
	return tx.JobRun.Create().
		SetJob(o.key).SetOk(o.ok).SetText(o.text).
		SetStartedAt(o.start).SetFinishedAt(o.end).SetDurationMs(o.dur).
		Exec(ctx)
}

// prune deletes the job's rows of one kind (ok) beyond the keep newest or
// last seen before cutoff.
func prune(ctx context.Context, tx *ent.Tx, key string, ok bool, keep int, cutoff time.Time) error {
	extra, err := tx.JobRun.Query().
		Where(jobrun.Job(key), jobrun.Ok(ok)).
		Order(ent.Desc(jobrun.FieldStartedAt), ent.Desc(jobrun.FieldID)).
		Offset(keep).
		IDs(ctx)
	if err != nil {
		return err
	}
	_, err = tx.JobRun.Delete().
		Where(jobrun.Job(key), jobrun.Ok(ok), jobrun.Or(jobrun.FinishedAtLT(cutoff), jobrun.IDIn(extra...))).
		Exec(ctx)
	return err
}

// failureText is the run's error, after what the run did get done, cut to
// MaxTextBytes.
func failureText(summary string, err error) string {
	text := err.Error()
	if summary != "" {
		text = "done: " + summary + "; failed: " + text
	}
	if len(text) <= MaxTextBytes {
		return text
	}
	cut := MaxTextBytes - len(truncMark)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + truncMark
}
