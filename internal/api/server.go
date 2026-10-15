// Package api implements the HTTP server: JSON API routes under /api and
// the embedded SPA.
package api

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"remna-control/internal/api/handlers"
	appmiddleware "remna-control/internal/api/middleware"
	"remna-control/internal/apperr"
)

type Deps struct {
	Handlers *handlers.Handlers
	// NoWeb disables serving the embedded SPA build (dev: the Bun dev server serves it).
	NoWeb bool
}

func New(deps *Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ReadTimeout:           15 * time.Second,
		WriteTimeout:          60 * time.Second,
		IdleTimeout:           120 * time.Second,
		BodyLimit:             256 << 20, // backup archives
		ErrorHandler:          errorHandler,
	})

	app.Use(recover.New())
	app.Use(appmiddleware.RequestLogger())

	RegisterRoutes(app, deps)
	return app
}

// errorHandler answers {"message", "code", "params"} with the error's
// status; the UI translates code (with params) and falls back to message.
func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	msg := err.Error()
	var fe *fiber.Error
	var ae *apperr.Error
	switch {
	case errors.As(err, &ae):
		if code = ae.Status; code == 0 {
			code = fiber.StatusBadRequest
		}
		return c.Status(code).JSON(fiber.Map{"message": msg, "code": ae.Code, "params": ae.Params})
	case errors.As(err, &fe):
		code = fe.Code
	}
	if code >= 500 {
		slog.Error("request failed", "method", c.Method(), "path", c.Path(), "error", err)
	}
	return c.Status(code).JSON(fiber.Map{"message": msg})
}
