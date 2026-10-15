package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/auditlog"
	"remna-control/internal/apperr"
	"remna-control/internal/fx"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
	"remna-control/internal/settings"
)

// GetSettings returns every setting, plus the read-only "_base_locked"
// ("true" once the books hold money, so the base currency is fixed).
func (h *Handlers) GetSettings(c *fiber.Ctx) error {
	ctx := c.UserContext()
	all, err := h.Settings.All(ctx)
	if err != nil {
		return err
	}
	locked, err := h.booksUsed(ctx)
	if err != nil {
		return err
	}
	all["_base_locked"] = strconv.FormatBool(locked)
	return c.JSON(all)
}

// booksUsed reports whether any money was recorded.
func (h *Handlers) booksUsed(ctx context.Context) (bool, error) {
	for _, exists := range []func(context.Context) (bool, error){
		h.DB.Payment.Query().Exist, h.DB.Expense.Query().Exist, h.DB.LedgerEntry.Query().Exist,
	} {
		if ok, err := exists(ctx); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func (h *Handlers) UpdateSettings(c *fiber.Ctx) error {
	var in map[string]string
	if err := bind(c, &in); err != nil {
		return err
	}
	ctx := c.UserContext()
	for k, v := range in {
		if strings.HasPrefix(k, "_") {
			continue // read-only flags echoed back by the UI
		}
		if _, known := settings.Defaults[k]; !known {
			return apperr.New("settings.unknown", "unknown setting {{key}}", "key", k)
		}
		if k == settings.BaseCurrency {
			if v = strings.ToUpper(strings.TrimSpace(v)); v != h.Settings.Base(ctx) {
				if err := h.checkBaseChange(ctx, v); err != nil {
					return err
				}
			}
		} else if _, err := strconv.ParseFloat(v, 64); err != nil {
			return apperr.New("settings.not_number", "{{key}} must be a number", "key", k)
		}
		if err := h.Settings.Set(c.UserContext(), k, v); err != nil {
			return err
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// checkBaseChange allows a new base currency only while the books are
// empty (amounts aren't converted) and only if rates for it are published.
func (h *Handlers) checkBaseChange(ctx context.Context, base string) error {
	if !currencyRe.MatchString(base) {
		return apperr.New("settings.bad_currency", "base currency must be a 3-letter ISO code")
	}
	if used, err := h.booksUsed(ctx); err != nil {
		return err
	} else if used {
		return apperr.Status(fiber.StatusConflict, "settings.base_locked", "the base currency can only change while there are no payments or expenses")
	}
	if base != "RUB" {
		if _, err := fx.SourceFor(base, &http.Client{Timeout: 15 * time.Second}).Fetch(ctx, time.Now()); err != nil {
			return apperr.Wrap(err, "settings.base_unsupported", "{{error}}")
		}
	}
	return nil
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (h *Handlers) ListAudit(c *fiber.Ctx) error {
	rows, err := h.DB.AuditLog.Query().Order(ent.Desc(auditlog.FieldID)).Limit(500).All(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(rows)
}

type webhookPayload struct {
	Scope string          `json:"scope"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// RemnawaveWebhook keeps the user cache fresh between full syncs. Signed
// with X-Remnawave-Signature (HMAC-SHA256 of the body).
func (h *Handlers) RemnawaveWebhook(c *fiber.Ctx) error {
	if h.Config.WebhookSecret == "" {
		return fiber.NewError(fiber.StatusNotFound, "webhook disabled")
	}
	body := c.Body()
	if !rwsync.VerifySignature(h.Config.WebhookSecret, body, c.Get("X-Remnawave-Signature")) {
		return fiber.NewError(fiber.StatusUnauthorized, "bad signature")
	}
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "bad payload")
	}
	if p.Scope != "user" {
		return c.SendStatus(fiber.StatusNoContent)
	}
	var u remnawave.User
	if err := json.Unmarshal(p.Data, &u); err != nil || u.ID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "bad user payload")
	}
	ctx := c.UserContext()
	var err error
	if p.Event == "user.deleted" {
		err = rwsync.MarkDeleted(ctx, h.DB, u.ID)
	} else {
		err = rwsync.Upsert(ctx, h.DB, &u)
	}
	if err != nil {
		slog.Error("webhook upsert failed", "event", p.Event, "user", u.ID, "error", err)
		return err
	}
	slog.Debug("webhook", "event", p.Event, "user", u.Username)
	return c.SendStatus(fiber.StatusNoContent)
}
