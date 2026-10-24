package handlers

import (
	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/ent/tariff"
	"remna-control/ent/tariffperiod"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/money"
	"remna-control/internal/store"
)

type PeriodView struct {
	Months int     `json:"months"`
	Days   int     `json:"days"`
	Price  float64 `json:"price"`
}

type TariffView struct {
	ID                int          `json:"id"`
	Kind              string       `json:"kind"`
	AddonID           *int         `json:"addon_id"`
	AddonName         string       `json:"addon_name"`
	Name              string       `json:"name"`
	Description       string       `json:"description"`
	MonthlyPrice      float64      `json:"monthly_price"`
	Active            bool         `json:"active"`
	SortOrder         int          `json:"sort_order"`
	ManageRw          bool         `json:"manage_rw"`
	TrafficLimitBytes int64        `json:"traffic_limit_bytes"`
	TrafficStrategy   string       `json:"traffic_strategy"`
	HwidLimit         *int         `json:"hwid_limit"`
	SquadUUIDs        []string     `json:"squad_uuids"`
	Periods           []PeriodView `json:"periods"`
	// IncludedAddonTariffIDs (base tariffs) are add-on tariffs that come
	// free with it.
	IncludedAddonTariffIDs []int   `json:"included_addon_tariff_ids"`
	Subscribers            int     `json:"subscribers"`
	Overridden             int     `json:"overridden"`
	MRR                    float64 `json:"mrr"`
}

func (h *Handlers) ListTariffs(c *fiber.Ctx) error {
	ctx := c.UserContext()
	ts, err := h.DB.Tariff.Query().WithAddon().WithIncludedAddons().WithPeriods(func(q *ent.TariffPeriodQuery) {
		q.Order(ent.Asc(tariffperiod.FieldMonths), ent.Asc(tariffperiod.FieldDays))
	}).Order(ent.Asc(tariff.FieldKind), ent.Asc(tariff.FieldSortOrder), ent.Asc(tariff.FieldMonthlyPrice)).All(ctx)
	if err != nil {
		return err
	}
	subs, err := h.loadSubscriptions(ctx)
	if err != nil {
		return err
	}
	type agg struct {
		n, overridden int
		mrr           float64
	}
	stats := map[int]*agg{}
	add := func(tid *int, price float64, override *float64, liveItem bool) {
		if tid == nil {
			return
		}
		a := stats[*tid]
		if a == nil {
			a = &agg{}
			stats[*tid] = a
		}
		a.n++
		if override != nil {
			a.overridden++
		}
		if liveItem {
			a.mrr += price
		}
	}
	for _, s := range subs {
		if s.CustomerArch {
			continue
		}
		add(s.TariffID, s.Price, s.PriceOverride, live(s.RwUser))
		for _, a := range s.Addons {
			add(a.TariffID, a.Price, a.PriceOverride, a.RwUser != nil && live(a.RwUser))
		}
	}
	out := make([]TariffView, 0, len(ts))
	for _, t := range ts {
		v := TariffView{
			ID: t.ID, Kind: t.Kind.String(), AddonID: t.AddonID, Name: t.Name, Description: t.Description,
			MonthlyPrice: money.ToMajor(t.MonthlyPrice), Active: t.Active, SortOrder: t.SortOrder,
			ManageRw: t.ManageRw, TrafficLimitBytes: t.TrafficLimitBytes, TrafficStrategy: t.TrafficStrategy,
			HwidLimit: t.HwidLimit, SquadUUIDs: t.SquadUuids, Periods: []PeriodView{},
			IncludedAddonTariffIDs: []int{},
		}
		for _, inc := range t.Edges.IncludedAddons {
			v.IncludedAddonTariffIDs = append(v.IncludedAddonTariffIDs, inc.ID)
		}
		if v.SquadUUIDs == nil {
			v.SquadUUIDs = []string{}
		}
		if t.Edges.Addon != nil {
			v.AddonName = t.Edges.Addon.Name
		}
		for _, p := range t.Edges.Periods {
			v.Periods = append(v.Periods, PeriodView{Months: p.Months, Days: p.Days, Price: money.ToMajor(p.Price)})
		}
		if a := stats[t.ID]; a != nil {
			v.Subscribers, v.Overridden, v.MRR = a.n, a.overridden, a.mrr
		}
		out = append(out, v)
	}
	return c.JSON(out)
}

type tariffInput struct {
	Kind              string       `json:"kind"`
	AddonID           *int         `json:"addon_id"`
	Name              string       `json:"name"`
	Description       string       `json:"description"`
	MonthlyPrice      float64      `json:"monthly_price"`
	Active            bool         `json:"active"`
	SortOrder         int          `json:"sort_order"`
	ManageRw          bool         `json:"manage_rw"`
	TrafficLimitBytes int64        `json:"traffic_limit_bytes"`
	TrafficStrategy   string       `json:"traffic_strategy"`
	HwidLimit         *int         `json:"hwid_limit"`
	SquadUUIDs        []string     `json:"squad_uuids"`
	Periods           []PeriodView `json:"periods"`
	// IncludedAddonTariffIDs: add-on tariffs included in a base tariff.
	IncludedAddonTariffIDs []int `json:"included_addon_tariff_ids"`
	// PriceChange, when the monthly price changes: "keep" pins the old
	// price as an override on current subscribers, "apply" moves everyone.
	PriceChange string `json:"price_change"`
}

func (in *tariffInput) validate() error {
	if in.Kind != "base" && in.Kind != "addon" {
		return apperr.New("tariff.bad_kind", "tariff kind must be base or addon")
	}
	if in.Kind == "addon" && in.AddonID == nil {
		return apperr.New("tariff.addon_required", "choose an add-on")
	}
	if in.Kind == "base" {
		in.AddonID = nil
	} else {
		in.IncludedAddonTariffIDs = nil
	}
	if in.TrafficStrategy == "" {
		in.TrafficStrategy = "NO_RESET"
	}
	return nil
}

func (h *Handlers) CreateTariff(c *fiber.Ctx) error {
	var in tariffInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	ctx := c.UserContext()
	var id int
	err := store.WithTx(ctx, h.DB, func(tx *ent.Tx) error {
		t, err := tx.Tariff.Create().
			SetKind(tariff.Kind(in.Kind)).SetNillableAddonID(in.AddonID).SetName(in.Name).SetDescription(in.Description).
			SetMonthlyPrice(money.FromMajor(in.MonthlyPrice)).SetActive(in.Active).SetSortOrder(in.SortOrder).
			SetManageRw(in.ManageRw).SetTrafficLimitBytes(in.TrafficLimitBytes).SetTrafficStrategy(in.TrafficStrategy).
			SetNillableHwidLimit(in.HwidLimit).SetSquadUuids(in.SquadUUIDs).
			Save(ctx)
		if err != nil {
			return err
		}
		id = t.ID
		if err := saveIncluded(ctx, tx, t.ID, in.IncludedAddonTariffIDs); err != nil {
			return err
		}
		return savePeriods(ctx, tx, t.ID, in.Periods)
	})
	audit.Log(ctx, h.DB, "tariff.create", "tariff", id, in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

func savePeriods(ctx fiberCtx, tx *ent.Tx, tariffID int, periods []PeriodView) error {
	if _, err := tx.TariffPeriod.Delete().Where(tariffperiod.TariffID(tariffID)).Exec(ctx); err != nil {
		return err
	}
	for _, p := range periods {
		// A plain month is the monthly price; empty terms mean nothing.
		if p.Months < 0 || p.Days < 0 || p.Months+p.Days == 0 || (p.Months == 1 && p.Days == 0) {
			continue
		}
		if err := tx.TariffPeriod.Create().SetTariffID(tariffID).SetMonths(p.Months).SetDays(p.Days).SetPrice(money.FromMajor(p.Price)).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// saveIncluded sets the add-on tariffs a base tariff includes: add-on
// tariffs only, at most one per add-on. Existing subscriptions pick the
// change up on their next tariff change.
func saveIncluded(ctx fiberCtx, tx *ent.Tx, tariffID int, ids []int) error {
	ts, err := tx.Tariff.Query().Where(tariff.IDIn(ids...)).All(ctx)
	if err != nil {
		return err
	}
	if len(ts) != len(ids) {
		return apperr.New("tariff.included_unknown", "included add-ons: unknown tariff")
	}
	seen := map[int]bool{}
	for _, t := range ts {
		if t.Kind != tariff.KindAddon || t.AddonID == nil {
			return apperr.New("tariff.included_not_addon", "only add-on tariffs can be included")
		}
		if seen[*t.AddonID] {
			return apperr.New("tariff.included_twice", "an add-on is included twice")
		}
		seen[*t.AddonID] = true
	}
	return tx.Tariff.UpdateOneID(tariffID).ClearIncludedAddons().AddIncludedAddonIDs(ids...).Exec(ctx)
}

func (h *Handlers) UpdateTariff(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in tariffInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	ctx := c.UserContext()
	old, err := h.DB.Tariff.Get(ctx, id)
	if err != nil {
		return badRequest(err)
	}
	newPrice := money.FromMajor(in.MonthlyPrice)
	err = store.WithTx(ctx, h.DB, func(tx *ent.Tx) error {
		if newPrice != old.MonthlyPrice && in.PriceChange == "keep" {
			if _, err := tx.Subscription.Update().
				Where(subscription.TariffID(id), subscription.PriceOverrideIsNil()).
				SetPriceOverride(old.MonthlyPrice).Save(ctx); err != nil {
				return err
			}
			if _, err := tx.SubscriptionAddon.Update().
				Where(subscriptionaddon.TariffID(id), subscriptionaddon.PriceOverrideIsNil()).
				SetPriceOverride(old.MonthlyPrice).Save(ctx); err != nil {
				return err
			}
		}
		q := tx.Tariff.UpdateOneID(id).
			SetName(in.Name).SetDescription(in.Description).SetMonthlyPrice(newPrice).
			SetActive(in.Active).SetSortOrder(in.SortOrder).SetManageRw(in.ManageRw).
			SetTrafficLimitBytes(in.TrafficLimitBytes).SetTrafficStrategy(in.TrafficStrategy).
			SetSquadUuids(in.SquadUUIDs)
		if in.HwidLimit != nil {
			q.SetHwidLimit(*in.HwidLimit)
		} else {
			q.ClearHwidLimit()
		}
		if err := q.Exec(ctx); err != nil {
			return err
		}
		if err := saveIncluded(ctx, tx, id, in.IncludedAddonTariffIDs); err != nil {
			return err
		}
		return savePeriods(ctx, tx, id, in.Periods)
	})
	audit.Log(ctx, h.DB, "tariff.update", "tariff", id, in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SyncIncluded: POST /tariffs/:id/sync-included {removed: disable|keep_paid}
// applies the tariff's included add-ons to its current subscribers.
func (h *Handlers) SyncIncluded(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in struct {
		Removed string `json:"removed"`
	}
	if err := bind(c, &in); err != nil {
		return err
	}
	n, err := h.Billing.SyncIncluded(c.UserContext(), id, in.Removed)
	if err != nil {
		return badRequest(err)
	}
	return c.JSON(fiber.Map{"subscriptions": n})
}

// SetTariffArchived: POST /tariffs/:id/archive and /restore.
func (h *Handlers) SetTariffArchived(archived bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := paramID(c, "id")
		if err != nil {
			return err
		}
		err = h.DB.Tariff.UpdateOneID(id).SetActive(!archived).Exec(c.UserContext())
		audit.Log(c.UserContext(), h.DB, "tariff.archive", "tariff", id, fiber.Map{"archived": archived}, err)
		if err != nil {
			return badRequest(err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func (h *Handlers) DeleteTariff(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	tf, err := h.DB.Tariff.Get(ctx, id)
	if err != nil {
		return badRequest(err)
	}
	if tf.Active {
		return apperr.Status(fiber.StatusConflict, "tariff.not_archived", "archive the tariff first; only archived tariffs can be deleted")
	}
	n1, _ := h.DB.Subscription.Query().Where(subscription.TariffID(id)).Count(ctx)
	n2, _ := h.DB.SubscriptionAddon.Query().Where(subscriptionaddon.TariffID(id)).Count(ctx)
	if n1+n2 > 0 {
		return apperr.Status(fiber.StatusConflict, "tariff.in_use", "the tariff is in use by subscriptions")
	}
	if _, err := h.DB.TariffPeriod.Delete().Where(tariffperiod.TariffID(id)).Exec(ctx); err != nil {
		return err
	}
	err = h.DB.Tariff.DeleteOneID(id).Exec(ctx)
	audit.Log(ctx, h.DB, "tariff.delete", "tariff", id, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
