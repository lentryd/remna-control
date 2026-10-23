package handlers

import (
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/extension"
	"remna-control/ent/rwuser"
	"remna-control/ent/subscriptionaddon"
	"remna-control/internal/audit"
	"remna-control/internal/billing"
	"remna-control/internal/money"
	"remna-control/internal/remnawave"
)

func (h *Handlers) ListSubscriptions(c *fiber.Ctx) error {
	out, err := h.loadSubscriptions(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(out)
}

type subscriptionInput struct {
	CustomerID    int      `json:"customer_id"`
	TariffID      *int     `json:"tariff_id"`
	RwUserID      *int     `json:"rw_user_id"`
	Label         string   `json:"label"`
	PriceOverride *float64 `json:"price_override"`
	AutoExtend    *bool    `json:"auto_extend"`
}

// CreateSubscription links an existing panel user (or none yet) to a
// customer; it doesn't touch the panel.
func (h *Handlers) CreateSubscription(c *fiber.Ctx) error {
	var in subscriptionInput
	if err := bind(c, &in); err != nil {
		return err
	}
	autoExtend := in.AutoExtend == nil || *in.AutoExtend
	sub, err := h.DB.Subscription.Create().
		SetCustomerID(in.CustomerID).SetNillableTariffID(in.TariffID).SetNillableRwUserID(in.RwUserID).
		SetLabel(in.Label).SetNillablePriceOverride(money.FromMajorPtr(in.PriceOverride)).
		SetAutoExtend(autoExtend).
		Save(c.UserContext())
	audit.Log(c.UserContext(), h.DB, "subscription.create", "customer", in.CustomerID, in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": sub.ID})
}

func (h *Handlers) UpdateSubscription(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in subscriptionInput
	if err := bind(c, &in); err != nil {
		return err
	}
	q := h.DB.Subscription.UpdateOneID(id).SetCustomerID(in.CustomerID).SetLabel(in.Label)
	if in.TariffID != nil {
		q.SetTariffID(*in.TariffID)
	} else {
		q.ClearTariffID()
	}
	if in.RwUserID != nil {
		q.SetRwUserID(*in.RwUserID)
	} else {
		q.ClearRwUserID()
	}
	if in.PriceOverride != nil {
		q.SetPriceOverride(money.FromMajor(*in.PriceOverride))
	} else {
		q.ClearPriceOverride()
	}
	if in.AutoExtend != nil {
		q.SetAutoExtend(*in.AutoExtend)
	}
	err = q.Exec(c.UserContext())
	audit.Log(c.UserContext(), h.DB, "subscription.update", "subscription", id, in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// DeleteSubscription unlinks the subscription (and its add-ons) here; the
// panel users stay untouched.
func (h *Handlers) DeleteSubscription(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	addons, err := h.DB.SubscriptionAddon.Query().Where(subscriptionaddon.SubscriptionID(id)).IDs(ctx)
	if err != nil {
		return err
	}
	if len(addons) > 0 {
		if _, err := h.DB.Extension.Update().Where(extension.SubscriptionAddonIDIn(addons...)).ClearSubscriptionAddonID().Save(ctx); err != nil {
			return err
		}
		if _, err := h.DB.SubscriptionAddon.Delete().Where(subscriptionAddonIDIn(addons)).Exec(ctx); err != nil {
			return err
		}
	}
	if _, err := h.DB.Extension.Update().Where(extension.SubscriptionID(id)).ClearSubscriptionID().Save(ctx); err != nil {
		return err
	}
	err = h.DB.Subscription.DeleteOneID(id).Exec(ctx)
	audit.Log(ctx, h.DB, "subscription.delete", "subscription", id, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type provisionRequest struct {
	CustomerID int     `json:"customer_id"`
	TariffID   int     `json:"tariff_id"`
	Username   string  `json:"username"`
	Label      string  `json:"label"`
	Months     int     `json:"months"`
	Days       int     `json:"days"`
	Amount     float64 `json:"amount"`
	AllowDebt  bool    `json:"allow_debt"`
}

// ProvisionSubscription creates a new panel user for a customer.
func (h *Handlers) ProvisionSubscription(c *fiber.Ctx) error {
	var req provisionRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	sub, err := h.Billing.ProvisionSubscription(c.UserContext(), billing.ProvisionInput{
		CustomerID: req.CustomerID, TariffID: req.TariffID, Username: req.Username, Label: req.Label,
		Months: req.Months, Days: req.Days, Amount: money.FromMajor(req.Amount), AllowDebt: req.AllowDebt,
	})
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": sub.ID})
}

type connectAddonRequest struct {
	TariffID  int        `json:"tariff_id"`
	Months    int        `json:"months"`
	Days      int        `json:"days"`
	Until     *time.Time `json:"until"`
	Amount    float64    `json:"amount"`
	AllowDebt bool       `json:"allow_debt"`
}

func (h *Handlers) ConnectAddon(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var req connectAddonRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	sa, err := h.Billing.ConnectAddon(c.UserContext(), billing.ConnectAddonInput{
		SubscriptionID: id, TariffID: req.TariffID, Months: req.Months, Days: req.Days, Until: req.Until,
		Amount: money.FromMajor(req.Amount), AllowDebt: req.AllowDebt,
	})
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": sa.ID})
}

type addonInput struct {
	TariffID      *int     `json:"tariff_id"`
	PriceOverride *float64 `json:"price_override"`
	AutoExtend    *bool    `json:"auto_extend"`
}

func (h *Handlers) UpdateSubscriptionAddon(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in addonInput
	if err := bind(c, &in); err != nil {
		return err
	}
	q := h.DB.SubscriptionAddon.UpdateOneID(id)
	if in.TariffID != nil {
		q.SetTariffID(*in.TariffID)
	}
	if in.PriceOverride != nil {
		q.SetPriceOverride(money.FromMajor(*in.PriceOverride))
	} else {
		q.ClearPriceOverride()
	}
	if in.AutoExtend != nil {
		q.SetAutoExtend(*in.AutoExtend)
	}
	err = q.Exec(c.UserContext())
	audit.Log(c.UserContext(), h.DB, "addon.update", "addon", id, in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) DeleteSubscriptionAddon(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	if _, err := h.DB.Extension.Update().Where(extension.SubscriptionAddonID(id)).ClearSubscriptionAddonID().Save(ctx); err != nil {
		return err
	}
	err = h.DB.SubscriptionAddon.DeleteOneID(id).Exec(ctx)
	audit.Log(ctx, h.DB, "addon.unlink", "addon", id, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func itemKind(c *fiber.Ctx) (string, int, error) {
	kind := c.Params("kind")
	if kind != billing.KindSubscription && kind != billing.KindAddon {
		return "", 0, fiber.NewError(fiber.StatusBadRequest, "kind: subscription | addon")
	}
	id, err := paramID(c, "id")
	return kind, id, err
}

// QuoteExtend prices an extension: GET ?months=&days=.
func (h *Handlers) QuoteExtend(c *fiber.Ctx) error {
	kind, id, err := itemKind(c)
	if err != nil {
		return err
	}
	months, _ := strconv.Atoi(c.Query("months", "1"))
	days, _ := strconv.Atoi(c.Query("days", "0"))
	q, err := h.Billing.QuoteExtend(c.UserContext(), kind, id, months, days)
	if err != nil {
		return badRequest(err)
	}
	periods := make([]PeriodView, 0, len(q.Periods))
	for _, p := range q.Periods {
		periods = append(periods, PeriodView{Months: p.Months, Days: p.Days, Price: money.ToMajor(p.Price)})
	}
	return c.JSON(fiber.Map{
		"customer_id": q.CustomerID,
		"amount":      money.ToMajor(q.Amount), "from": q.From, "to": q.To,
		"monthly": money.ToMajor(q.Monthly), "periods": periods, "balance": money.ToMajor(q.Balance),
	})
}

type extendRequest struct {
	Months    int     `json:"months"`
	Days      int     `json:"days"`
	Amount    float64 `json:"amount"`
	AllowDebt bool    `json:"allow_debt"`
}

func (h *Handlers) Extend(c *fiber.Ctx) error {
	kind, id, err := itemKind(c)
	if err != nil {
		return err
	}
	var req extendRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	res, err := h.Billing.Extend(c.UserContext(), billing.ExtendInput{
		Kind: kind, ID: id, Months: req.Months, Days: req.Days,
		Amount: money.FromMajor(req.Amount), AllowDebt: req.AllowDebt,
	})
	if err != nil {
		return badRequest(err)
	}
	return c.JSON(extensionResult(*res))
}

func (h *Handlers) QuoteTariff(c *fiber.Ctx) error {
	kind, id, err := itemKind(c)
	if err != nil {
		return err
	}
	tariffID, _ := strconv.Atoi(c.Query("tariff_id"))
	q, err := h.Billing.QuoteTariffChange(c.UserContext(), kind, id, tariffID)
	if err != nil {
		return badRequest(err)
	}
	addons := make([]fiber.Map, 0, len(q.Addons))
	for _, a := range q.Addons {
		addons = append(addons, fiber.Map{
			"subscription_addon_id": a.SubscriptionAddonID, "addon_id": a.AddonID, "addon_name": a.AddonName,
			"tariff_name": a.TariffName, "action": a.Action, "credit": money.ToMajor(a.Credit),
		})
	}
	return c.JSON(fiber.Map{
		"old_monthly": money.ToMajor(q.OldMonthly), "new_monthly": money.ToMajor(q.NewMonthly),
		"expire_at": q.ExpireAt, "surcharge": money.ToMajor(q.Surcharge), "addons": addons,
	})
}

type changeTariffRequest struct {
	TariffID      int     `json:"tariff_id"`
	Surcharge     float64 `json:"surcharge"`
	ClearOverride bool    `json:"clear_override"`
	// RemovedAddons: subscription add-on id → "disable" | "keep_paid".
	RemovedAddons map[int]string `json:"removed_addons"`
}

func (h *Handlers) ChangeTariff(c *fiber.Ctx) error {
	kind, id, err := itemKind(c)
	if err != nil {
		return err
	}
	var req changeTariffRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := h.Billing.ChangeTariff(c.UserContext(), billing.ChangeTariffInput{
		Kind: kind, ID: id, TariffID: req.TariffID, Surcharge: money.FromMajor(req.Surcharge), ClearOverride: req.ClearOverride,
		RemovedAddons: req.RemovedAddons,
	}); err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) SetEnabled(enabled bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		kind, id, err := itemKind(c)
		if err != nil {
			return err
		}
		if err := h.Billing.SetEnabled(c.UserContext(), kind, id, enabled); err != nil {
			return badRequest(err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// RwUserRow is a cached panel user with what it's linked to.
type RwUserRow struct {
	RwUserView
	SubscriptionID *int `json:"subscription_id"`
	AddonID        *int `json:"subscription_addon_id"`
	// ParentID is the subscription that owns the add-on (or the subscription itself).
	ParentID     *int   `json:"parent_subscription_id"`
	CustomerID   *int   `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	Linked       bool   `json:"linked"`
}

func (h *Handlers) ListRwUsers(c *fiber.Ctx) error {
	users, err := h.DB.RwUser.Query().
		Where(rwuser.Deleted(false)).
		WithSubscription(func(q *ent.SubscriptionQuery) { q.WithCustomer() }).
		WithSubscriptionAddon(func(q *ent.SubscriptionAddonQuery) {
			q.WithSubscription(func(q *ent.SubscriptionQuery) { q.WithCustomer() })
		}).
		Order(ent.Asc(rwuser.FieldUsername)).
		All(c.UserContext())
	if err != nil {
		return err
	}
	out := make([]RwUserRow, 0, len(users))
	for _, u := range users {
		r := RwUserRow{RwUserView: *rwUserView(u)}
		var sub *ent.Subscription
		if s := u.Edges.Subscription; s != nil {
			r.SubscriptionID, sub = &s.ID, s
		} else if sa := u.Edges.SubscriptionAddon; sa != nil {
			r.AddonID, sub = &sa.ID, sa.Edges.Subscription
		}
		if sub != nil {
			r.Linked, r.ParentID = true, &sub.ID
			if sub.Edges.Customer != nil {
				r.CustomerID, r.CustomerName = &sub.Edges.Customer.ID, sub.Edges.Customer.Name
			}
		}
		out = append(out, r)
	}
	return c.JSON(out)
}

func (h *Handlers) SyncNow(c *fiber.Ctx) error {
	return h.runJob(c, "panel_users")
}

func (h *Handlers) SyncStatus(c *fiber.Ctx) error {
	last, syncErr, err := h.panelSync(c.UserContext())
	if err != nil {
		return err
	}
	m := fiber.Map{"last_sync": nil, "error": syncErr}
	if last != nil {
		m["last_sync"] = last.Format(time.RFC3339)
	}
	return c.JSON(m)
}

// panelSync is the panel user sync's last success (nil before the first)
// and, only while its latest run failed, that run's error (else nil).
func (h *Handlers) panelSync(ctx fiberCtx) (*time.Time, any, error) {
	v, err := h.Jobs.Status(ctx, "panel_users")
	if err != nil {
		return nil, nil, err
	}
	if v.LastOK != nil && !*v.LastOK {
		return v.LastSuccessAt, v.LastError, nil
	}
	return v.LastSuccessAt, nil, nil
}

func (h *Handlers) ListSquads(c *fiber.Ctx) error {
	squads, err := h.RW.InternalSquads(c.UserContext())
	if err != nil {
		return badRequest(err)
	}
	return c.JSON(squads)
}

func (h *Handlers) ListNodes(c *fiber.Ctx) error {
	nodes, err := h.RW.Nodes(c.UserContext())
	if err != nil {
		return badRequest(err)
	}
	return c.JSON(nodes)
}

// ListInfra returns the panel's Infra Billing providers and billing nodes.
// Errors (e.g. a token without the infra-billing scope) come back in "error"
// so the UI can fall back to free-text providers.
func (h *Handlers) ListInfra(c *fiber.Ctx) error {
	ctx := c.UserContext()
	out := fiber.Map{"providers": []remnawave.InfraProvider{}, "billing_nodes": []remnawave.InfraBillingNode{}, "error": nil}
	providers, err := h.RW.InfraProviders(ctx)
	if err != nil {
		out["error"] = err.Error()
		return c.JSON(out)
	}
	out["providers"] = providers
	if nodes, err := h.RW.InfraBillingNodes(ctx); err != nil {
		out["error"] = err.Error()
	} else {
		out["billing_nodes"] = nodes
	}
	return c.JSON(out)
}

type trafficAnchorInput struct {
	Anchor time.Time `json:"anchor"`
}

func (h *Handlers) setTrafficAnchor(c *fiber.Ctx, kind string) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in trafficAnchorInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if in.Anchor.IsZero() {
		return badRequest(errors.New("anchor is required"))
	}
	err = h.Billing.SetTrafficAnchor(c.UserContext(), kind, id, in.Anchor)
	audit.Log(c.UserContext(), h.DB, "subscription.traffic_anchor", kind, id, in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) SetSubscriptionTrafficAnchor(c *fiber.Ctx) error {
	return h.setTrafficAnchor(c, billing.KindSubscription)
}

func (h *Handlers) SetAddonTrafficAnchor(c *fiber.Ctx) error {
	return h.setTrafficAnchor(c, billing.KindAddon)
}
