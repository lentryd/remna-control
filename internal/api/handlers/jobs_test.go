package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent/auditlog"
	"remna-control/internal/jobs"
)

// setupJobs mounts the jobs endpoints over a manager holding the given stub
// jobs, registered in the given order.
func setupJobs(t *testing.T, fns map[string]jobs.Func, order ...string) (*fiber.App, *Handlers) {
	t.Helper()
	db := openDB(t)
	m := jobs.New(db, true)
	for _, k := range order {
		m.Register(jobs.Job{Key: k, Every: time.Minute, Tick: fns[k]})
	}
	h := &Handlers{DB: db, Jobs: m}
	app := testApp()
	app.Get("/jobs", h.ListJobs)
	app.Get("/jobs/:key/runs", h.JobRuns)
	app.Post("/jobs/:key/run", h.RunJobNow)
	return app, h
}

func getJSON(t *testing.T, app *fiber.App, method, path string, out any) int {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(method, path, nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func okJob(context.Context) (jobs.Result, error) {
	return jobs.Result{Summary: "did it", Changed: true}, nil
}

func TestListJobs_RegistrationOrder(t *testing.T) {
	app, _ := setupJobs(t, map[string]jobs.Func{"b": okJob, "a": okJob}, "b", "a")
	var out []jobs.View
	if code := getJSON(t, app, "GET", "/jobs", &out); code != fiber.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(out) != 2 || out[0].Key != "b" || out[1].Key != "a" {
		t.Fatalf("order: %+v", out)
	}
	if !out[0].Enabled || out[0].IntervalSeconds == nil || *out[0].IntervalSeconds != 60 || out[0].LastOK != nil {
		t.Errorf("view: %+v", out[0])
	}
}

func TestRunJob_RecordsAndAudits(t *testing.T) {
	app, h := setupJobs(t, map[string]jobs.Func{"a": okJob}, "a")
	var v jobs.View
	if code := getJSON(t, app, "POST", "/jobs/a/run", &v); code != fiber.StatusOK {
		t.Fatalf("status %d", code)
	}
	if v.Key != "a" || v.LastOK == nil || !*v.LastOK || v.LastSummary != "did it" || v.Runs != 1 {
		t.Fatalf("view: %+v", v)
	}
	rows := h.DB.AuditLog.Query().Where(auditlog.Action("job.run")).AllX(context.Background())
	if len(rows) != 1 || rows[0].Entity != "job:a" || !rows[0].Ok {
		t.Fatalf("audit: %+v", rows)
	}
}

func TestRunJob_FailureIsData(t *testing.T) {
	app, h := setupJobs(t, map[string]jobs.Func{"a": func(context.Context) (jobs.Result, error) {
		return jobs.Result{}, errors.New("boom")
	}}, "a")
	var v jobs.View
	if code := getJSON(t, app, "POST", "/jobs/a/run", &v); code != fiber.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if v.LastOK == nil || *v.LastOK || v.LastError != "boom" || v.Failures != 1 {
		t.Fatalf("view: %+v", v)
	}
	rows := h.DB.AuditLog.Query().Where(auditlog.Action("job.run")).AllX(context.Background())
	if len(rows) != 1 || rows[0].Ok || rows[0].Error != "boom" {
		t.Fatalf("audit: %+v", rows)
	}
}

func TestRunJob_RunningUnknown(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	app, h := setupJobs(t, map[string]jobs.Func{"a": func(context.Context) (jobs.Result, error) {
		started <- struct{}{}
		<-release
		return jobs.Result{}, nil
	}}, "a")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.Jobs.Run(context.Background(), "a")
	}()
	<-started

	if code, body := call(t, app, "POST", "/jobs/a/run"); code != fiber.StatusConflict || body["code"] != "jobs.running" {
		t.Errorf("running: %d %v", code, body)
	}
	close(release)
	<-done

	if code, body := call(t, app, "POST", "/jobs/nope/run"); code != fiber.StatusNotFound || body["code"] != "jobs.unknown" {
		t.Errorf("unknown run: %d %v", code, body)
	}
	if code, body := call(t, app, "GET", "/jobs/nope/runs"); code != fiber.StatusNotFound || body["code"] != "jobs.unknown" {
		t.Errorf("unknown runs: %d %v", code, body)
	}
	if n := h.DB.AuditLog.Query().Where(auditlog.Action("job.run")).CountX(context.Background()); n != 0 {
		t.Errorf("audit rows for 404/409: %d", n)
	}
}

func TestJobRuns_OnlyFailures(t *testing.T) {
	calls := 0
	app, _ := setupJobs(t, map[string]jobs.Func{"a": func(context.Context) (jobs.Result, error) {
		calls++
		if calls == 2 {
			return jobs.Result{}, errors.New("boom")
		}
		return jobs.Result{Summary: "ok", Changed: true}, nil
	}}, "a")
	for range 3 {
		getJSON(t, app, "POST", "/jobs/a/run", nil)
	}
	var all, bad []jobs.RunRow
	if code := getJSON(t, app, "GET", "/jobs/a/runs", &all); code != fiber.StatusOK || len(all) != 3 {
		t.Fatalf("all: %d %+v", code, all)
	}
	if code := getJSON(t, app, "GET", "/jobs/a/runs?ok=false", &bad); code != fiber.StatusOK || len(bad) != 1 || bad[0].OK || bad[0].Text != "boom" {
		t.Fatalf("failures: %d %+v", code, bad)
	}
}
