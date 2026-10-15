package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"remna-control/internal/api/handlers"
	appmiddleware "remna-control/internal/api/middleware"
	"remna-control/internal/config"
)

// Every scope catalog endpoint is routed (the bare app answers the
// missing token with 500, an unknown path with 404).
func TestPublicRoutes(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app, &Deps{Handlers: &handlers.Handlers{Config: &config.Config{JWTSecret: "x"}}, NoWeb: true})
	for _, r := range appmiddleware.ScopeCatalog {
		for _, ep := range r.Endpoints {
			resp, err := app.Test(httptest.NewRequest(ep.Method, strings.NewReplacer("{kind}", "subscription", "{id}", "1", "{name}", "x.zip").Replace(ep.Path), nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode == fiber.StatusNotFound || resp.StatusCode < 400 {
				t.Errorf("%s %s: status %d", ep.Method, ep.Path, resp.StatusCode)
			}
		}
	}
}
