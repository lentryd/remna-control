package handlers

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent/tariff"
	"remna-control/internal/store"
)

func TestDeleteSubscription_UnlinksAddons(t *testing.T) {
	ctx := context.Background()
	db, _, err := store.OpenDB(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := db.Customer.Create().SetName("Alice").SaveX(ctx)
	a := db.Addon.Create().SetName("white").SetPrefix("white_").SaveX(ctx)
	tr := db.Tariff.Create().SetKind(tariff.KindAddon).SetAddonID(a.ID).SetName("White").SetMonthlyPrice(100).SaveX(ctx)
	sub := db.Subscription.Create().SetCustomerID(c.ID).SaveX(ctx)
	db.SubscriptionAddon.Create().SetSubscriptionID(sub.ID).SetAddonID(a.ID).SetTariffID(tr.ID).SaveX(ctx)

	h := &Handlers{DB: db}
	app := fiber.New()
	app.Delete("/subscriptions/:id", h.DeleteSubscription)
	resp, err := app.Test(httptest.NewRequest("DELETE", "/subscriptions/"+strconv.Itoa(sub.ID), nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if n := db.Subscription.Query().CountX(ctx); n != 0 {
		t.Fatalf("subscriptions left: %d", n)
	}
	if n := db.SubscriptionAddon.Query().CountX(ctx); n != 0 {
		t.Fatalf("add-ons left: %d", n)
	}
}
