// Package handlers implements the JSON API. One file per area; each handler
// parses the request, calls a service or Ent, and responds with DTOs where
// money is in rubles (float) and dates are RFC 3339.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/internal/apperr"
	"remna-control/internal/backup"
	"remna-control/internal/billing"
	"remna-control/internal/config"
	"remna-control/internal/expenses"
	"remna-control/internal/fx"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
	"remna-control/internal/settings"
)

type Handlers struct {
	DB        *ent.Client
	SQL       *sql.DB // same database, for table-level backups
	RW        *remnawave.Client
	Billing   *billing.Service
	Expenses  *expenses.Service
	FX        *fx.Service
	Sync      *rwsync.Service
	Settings  *settings.Store
	Snapshots *backup.Snapshots
	Config    *config.Config
	Version   string
}

func paramID(c *fiber.Ctx, name string) (int, error) {
	id, err := strconv.Atoi(c.Params(name))
	if err != nil || id <= 0 {
		return 0, apperr.New("bad_id", "invalid id")
	}
	return id, nil
}

func bind(c *fiber.Ctx, v any) error {
	if err := c.BodyParser(v); err != nil {
		return apperr.Wrap(err, "bad_request", "invalid request: {{error}}")
	}
	return nil
}

// badRequest turns service validation errors into 400s; Ent not-found into
// 404s; everything else stays a 500.
func badRequest(err error) error {
	if err == nil {
		return nil
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return err
	}
	var apiErr *remnawave.APIError
	var ae *apperr.Error
	if errors.As(err, &ae) {
		// A coded error caused by the panel is the panel's fault.
		if ae.Status == fiber.StatusBadRequest && errors.As(err, &apiErr) {
			c := *ae // copy: package-level errors are shared
			c.Status = fiber.StatusBadGateway
			return &c
		}
		return err
	}
	if ent.IsNotFound(err) {
		return apperr.Status(fiber.StatusNotFound, "not_found", "not found")
	}
	if ent.IsConstraintError(err) {
		return apperr.Wrap(err, "conflict", "data conflict: {{error}}").WithStatus(fiber.StatusConflict)
	}
	if errors.As(err, &apiErr) {
		return apperr.Wrap(err, "panel.error", "Remnawave: {{error}}").WithStatus(fiber.StatusBadGateway)
	}
	return fiber.NewError(fiber.StatusBadRequest, err.Error())
}

// Date is a JSON date accepting "2006-01-02" or RFC 3339.
type Date struct{ time.Time }

func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			d.Time = t
			return nil
		}
	}
	return apperr.New("bad_date", "invalid date {{value}}", "value", s)
}

func (d Date) Ptr() *time.Time {
	if d.IsZero() {
		return nil
	}
	t := d.Time
	return &t
}

// fiberCtx is the context type used by helpers that only need a context.
type fiberCtx = context.Context
