package api

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"remna-control/internal/api/handlers"
	appmiddleware "remna-control/internal/api/middleware"
	"remna-control/web"
)

// RegisterRoutes wires the route table. The app owns its domain: JSON
// endpoints are under /api, the SPA is served for the rest.
func RegisterRoutes(app *fiber.App, deps *Deps) {
	h := deps.Handlers

	api := app.Group("/api")

	api.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })
	api.Post("/auth/login", h.Login)
	api.Post("/auth/logout", h.Logout)
	api.Post("/webhooks/remnawave", h.RemnawaveWebhook)

	// Public API for other services, authorized by API tokens. Registered
	// before the session group, whose middleware covers all of /api.
	registerPublic(api.Group("/v1"), h)

	a := api.Group("", appmiddleware.RequireSession(h.Config.JWTSecret))
	a.Get("/auth/me", h.Me)
	a.Get("/dashboard", h.Dashboard)

	a.Get("/customers", h.ListCustomers)
	a.Post("/customers", h.CreateCustomer)
	a.Get("/customers/:id", h.GetCustomer)
	a.Put("/customers/:id", h.UpdateCustomer)
	a.Delete("/customers/:id", h.DeleteCustomer)
	a.Post("/customers/:id/payments/preview", h.PreviewPayment)
	a.Post("/customers/:id/payments", h.CommitPayment)
	a.Post("/customers/:id/adjust", h.AdjustBalance)

	a.Get("/payments", h.ListPayments)
	a.Put("/payments/:id", h.UpdatePayment)
	a.Delete("/payments/:id", h.DeletePayment)

	a.Get("/referrals/tree", h.ReferralTree)
	a.Get("/referrals/accruals", h.ListAccruals)

	a.Get("/subscriptions", h.ListSubscriptions)
	a.Post("/subscriptions", h.CreateSubscription)
	a.Post("/subscriptions/provision", h.ProvisionSubscription)
	a.Put("/subscriptions/:id", h.UpdateSubscription)
	a.Delete("/subscriptions/:id", h.DeleteSubscription)
	a.Put("/subscriptions/:id/traffic-anchor", h.SetSubscriptionTrafficAnchor)
	a.Post("/subscriptions/:id/addons", h.ConnectAddon)
	a.Put("/subscription-addons/:id", h.UpdateSubscriptionAddon)
	a.Delete("/subscription-addons/:id", h.DeleteSubscriptionAddon)
	a.Put("/subscription-addons/:id/traffic-anchor", h.SetAddonTrafficAnchor)

	// kind: subscription | addon
	a.Get("/items/:kind/:id/quote", h.QuoteExtend)
	a.Post("/items/:kind/:id/extend", h.Extend)
	a.Get("/items/:kind/:id/tariff-quote", h.QuoteTariff)
	a.Post("/items/:kind/:id/tariff", h.ChangeTariff)
	a.Post("/items/:kind/:id/enable", h.SetEnabled(true))
	a.Post("/items/:kind/:id/disable", h.SetEnabled(false))

	a.Get("/tariffs", h.ListTariffs)
	a.Post("/tariffs", h.CreateTariff)
	a.Put("/tariffs/:id", h.UpdateTariff)
	a.Delete("/tariffs/:id", h.DeleteTariff)
	a.Post("/tariffs/:id/sync-included", h.SyncIncluded)
	a.Get("/addons", h.ListAddons)
	a.Post("/addons", h.CreateAddon)
	a.Put("/addons/:id", h.UpdateAddon)
	a.Delete("/addons/:id", h.DeleteAddon)

	a.Get("/api-tokens", h.ListTokens)
	a.Get("/api-tokens/scopes", h.TokenScopes)
	a.Post("/api-tokens", h.CreateToken)
	a.Delete("/api-tokens/:id", h.DeleteToken)

	a.Get("/rw/users", h.ListRwUsers)
	a.Get("/rw/squads", h.ListSquads)
	a.Get("/rw/nodes", h.ListNodes)
	a.Get("/rw/infra", h.ListInfra)
	a.Get("/rw/sync", h.SyncStatus)
	a.Post("/rw/sync", h.SyncNow)

	a.Get("/jobs", h.ListJobs)
	a.Get("/jobs/:key/runs", h.JobRuns)
	a.Post("/jobs/:key/run", h.RunJobNow)

	a.Get("/expenses", h.ListExpenses)
	a.Post("/expenses", h.CreateExpense)
	a.Put("/expenses/:id", h.UpdateExpense)
	a.Delete("/expenses/:id", h.DeleteExpense)
	a.Get("/expenses/providers", h.ProviderReport)

	a.Get("/expense-items", h.ListExpenseItems)
	a.Post("/expense-items", h.CreateExpenseItem)
	a.Put("/expense-items/:id", h.UpdateExpenseItem)
	a.Delete("/expense-items/:id", h.DeleteExpenseItem)
	a.Get("/expense-items/:id/metered", h.MeteredSummary)
	a.Post("/expense-items/:id/close-period", h.ClosePeriod)
	a.Post("/traffic/sync", h.SyncTraffic)

	a.Get("/fx/rate", h.FxRate)
	a.Get("/settings", h.GetSettings)
	a.Put("/settings", h.UpdateSettings)
	a.Get("/audit", h.ListAudit)

	a.Get("/backup/categories", h.BackupCategories)
	a.Get("/backup/export", h.ExportBackup)
	a.Post("/backup/inspect", h.InspectBackup)
	a.Post("/backup/import", h.ImportBackup)
	a.Get("/backup/snapshots", h.ListSnapshots)
	a.Post("/backup/snapshots", h.CreateSnapshot)
	a.Get("/backup/snapshots/:name", h.DownloadSnapshot)
	a.Delete("/backup/snapshots/:name", h.DeleteSnapshot)
	a.Get("/backup/snapshots/:name/inspect", h.InspectSnapshot)
	a.Post("/backup/snapshots/:name/restore", h.RestoreSnapshot)

	api.Use(func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusNotFound, "unknown endpoint")
	})

	if deps.NoWeb {
		return
	}

	// Registered after the API: the SPA files, precompressed at build time.
	static, err := staticHandler(web.Dist)
	if err != nil {
		panic(err)
	}
	app.Use(static)
}

// registerPublic wires every endpoint of the token scope catalog to the
// handler serving it; the admin handlers are reused as they are.
func registerPublic(v1 fiber.Router, h *handlers.Handlers) {
	byKey := map[string]fiber.Handler{
		"customers:list":            h.ListCustomers,
		"customers:lookup":          h.LookupCustomer,
		"customers:get":             h.GetCustomer,
		"customers:create":          h.CreateCustomer,
		"customers:update":          h.UpdateCustomer,
		"customers:delete":          h.DeleteCustomer,
		"customers:payment_preview": h.PreviewPayment,
		"customers:pay":             h.CommitPayment,
		"customers:adjust":          h.AdjustBalance,

		"payments:list":   h.ListPayments,
		"payments:update": h.UpdatePayment,
		"payments:delete": h.DeletePayment,

		"subscriptions:list":                 h.ListSubscriptions,
		"subscriptions:create":               h.CreateSubscription,
		"subscriptions:provision":            h.ProvisionSubscription,
		"subscriptions:update":               h.UpdateSubscription,
		"subscriptions:delete":               h.DeleteSubscription,
		"subscriptions:connect_addon":        h.ConnectAddon,
		"subscriptions:update_addon":         h.UpdateSubscriptionAddon,
		"subscriptions:delete_addon":         h.DeleteSubscriptionAddon,
		"subscriptions:traffic_anchor":       h.SetSubscriptionTrafficAnchor,
		"subscriptions:addon_traffic_anchor": h.SetAddonTrafficAnchor,

		"items:quote":        h.QuoteExtend,
		"items:extend":       h.Extend,
		"items:tariff_quote": h.QuoteTariff,
		"items:tariff":       h.ChangeTariff,
		"items:enable":       h.SetEnabled(true),
		"items:disable":      h.SetEnabled(false),

		"tariffs:list":          h.ListTariffs,
		"tariffs:create":        h.CreateTariff,
		"tariffs:update":        h.UpdateTariff,
		"tariffs:delete":        h.DeleteTariff,
		"tariffs:sync_included": h.SyncIncluded,

		"addons:list":   h.PublicAddons,
		"addons:full":   h.ListAddons,
		"addons:create": h.CreateAddon,
		"addons:update": h.UpdateAddon,
		"addons:delete": h.DeleteAddon,

		"referrals:tree":     h.ReferralTree,
		"referrals:accruals": h.ListAccruals,

		"expenses:list":              h.ListExpenses,
		"expenses:create":            h.CreateExpense,
		"expenses:update":            h.UpdateExpense,
		"expenses:delete":            h.DeleteExpense,
		"expenses:providers":         h.ProviderReport,
		"expenses:items":             h.ListExpenseItems,
		"expenses:item_create":       h.CreateExpenseItem,
		"expenses:item_update":       h.UpdateExpenseItem,
		"expenses:item_delete":       h.DeleteExpenseItem,
		"expenses:item_metered":      h.MeteredSummary,
		"expenses:item_close_period": h.ClosePeriod,
		"expenses:traffic_sync":      h.SyncTraffic,

		"remnawave:users":       h.ListRwUsers,
		"remnawave:squads":      h.ListSquads,
		"remnawave:nodes":       h.ListNodes,
		"remnawave:infra":       h.ListInfra,
		"remnawave:sync_status": h.SyncStatus,
		"remnawave:sync":        h.SyncNow,

		"stats:dashboard": h.Dashboard,
		"stats:fx_rate":   h.FxRate,
		"stats:settings":  h.GetSettings,
		"stats:audit":     h.ListAudit,

		"backups:list":     h.ListSnapshots,
		"backups:download": h.DownloadSnapshot,
		"backups:create":   h.CreateAndSendSnapshot,
	}
	for _, r := range appmiddleware.ScopeCatalog {
		for _, ep := range r.Endpoints {
			handler, ok := byKey[ep.Key]
			if !ok {
				panic(fmt.Sprintf("public API: no handler for %s", ep.Key))
			}
			// "/api/v1/customers/{id}" -> "/customers/:id"
			path := strings.TrimPrefix(ep.Path, "/api/v1")
			path = strings.NewReplacer("{", ":", "}", "").Replace(path)
			v1.Add(ep.Method, path, appmiddleware.RequireToken(h.DB, ep.Key), handler)
		}
	}
}
