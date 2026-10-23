package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/jobs"
)

// errJobUnknown answers a request for a job key that isn't registered.
var errJobUnknown = apperr.Status(fiber.StatusNotFound, "jobs.unknown", "unknown job")

// jobErr maps the jobs package's sentinel errors to API errors.
func jobErr(err error) error {
	if errors.Is(err, jobs.ErrUnknown) {
		return errJobUnknown
	}
	return badRequest(err)
}

// ListJobs returns every background job with its schedule and last run.
func (h *Handlers) ListJobs(c *fiber.Ctx) error {
	out, err := h.Jobs.List(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(out)
}

// JobRuns returns a job's history, newest first; ?ok=false keeps failures.
func (h *Handlers) JobRuns(c *fiber.Ctx) error {
	out, err := h.Jobs.History(c.UserContext(), c.Params("key"), c.Query("ok") == "false")
	if err != nil {
		return jobErr(err)
	}
	return c.JSON(out)
}

// RunJobNow runs a job now. A failed run is still a 200: the failure is in
// the returned view (last_ok, last_error), not a transport error.
func (h *Handlers) RunJobNow(c *fiber.Ctx) error {
	ctx, key := c.UserContext(), c.Params("key")
	v, err := h.Jobs.Run(ctx, key)
	if errors.Is(err, jobs.ErrUnknown) || errors.Is(err, jobs.ErrRunning) {
		return jobErr(err)
	}
	audit.Log(ctx, h.DB, "job.run", "job:"+key, 0, nil, err)
	return c.JSON(v)
}
