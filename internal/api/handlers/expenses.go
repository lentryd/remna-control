package handlers

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/expense"
	"remna-control/ent/expenseitem"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/expenses"
	"remna-control/internal/metered"
	"remna-control/internal/money"
)

type ExpenseView struct {
	ID            int       `json:"id"`
	Date          time.Time `json:"date"`
	Provider      string    `json:"provider"`
	ProviderUUID  string    `json:"provider_uuid"`
	ItemID        *int      `json:"expense_item_id"`
	ItemName      string    `json:"item_name"`
	Kind          string    `json:"kind"`
	OrigAmount    float64   `json:"orig_amount"`
	OrigCurrency  string    `json:"orig_currency"`
	FxRate        float64   `json:"fx_rate"`
	FeePercent    float64   `json:"fee_percent"`
	SharePercent  float64   `json:"share_percent"`
	RubAmount     float64   `json:"rub_amount"`
	RefundOfID    *int      `json:"refund_of_id"`
	RefundedTotal float64   `json:"refunded_total"`
	CalcRubAmount *float64  `json:"calc_rub_amount"`
	MeteredGB     *float64  `json:"metered_gb"`
	Period        string    `json:"period"`
	Note          string    `json:"note"`
}

func (h *Handlers) ListExpenses(c *fiber.Ctx) error {
	ctx := c.UserContext()
	rows, err := h.DB.Expense.Query().Order(ent.Desc(expense.FieldDate), ent.Desc(expense.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	items, err := h.DB.ExpenseItem.Query().All(ctx)
	if err != nil {
		return err
	}
	itemNames := map[int]string{}
	for _, it := range items {
		itemNames[it.ID] = it.Name
	}
	refunded := map[int]int64{}
	for _, e := range rows {
		if e.RefundOfID != nil {
			refunded[*e.RefundOfID] += -e.RubAmount
		}
	}
	out := make([]ExpenseView, 0, len(rows))
	for _, e := range rows {
		v := ExpenseView{
			ID: e.ID, Date: e.Date, Provider: e.Provider, ProviderUUID: e.RwProviderUUID, ItemID: e.ExpenseItemID, Kind: e.Kind.String(),
			OrigAmount: money.ToMajor(e.OrigAmount), OrigCurrency: e.OrigCurrency, FxRate: e.FxRate,
			FeePercent: e.FeePercent, SharePercent: e.SharePercent, RubAmount: money.ToMajor(e.RubAmount),
			RefundOfID: e.RefundOfID, RefundedTotal: money.ToMajor(refunded[e.ID]),
			CalcRubAmount: money.ToMajorPtr(e.CalcRubAmount), MeteredGB: e.MeteredGB, Period: e.Period, Note: e.Note,
		}
		if e.ExpenseItemID != nil {
			v.ItemName = itemNames[*e.ExpenseItemID]
		}
		out = append(out, v)
	}
	return c.JSON(out)
}

type expenseRequest struct {
	Date         Date     `json:"date"`
	Provider     string   `json:"provider"`
	ProviderUUID string   `json:"provider_uuid"`
	ItemID       *int     `json:"expense_item_id"`
	Kind         string   `json:"kind"`
	OrigAmount   float64  `json:"orig_amount"`
	OrigCurrency string   `json:"orig_currency"`
	FxRate       *float64 `json:"fx_rate"`
	FeePercent   float64  `json:"fee_percent"`
	SharePercent float64  `json:"share_percent"`
	RefundOfID   *int     `json:"refund_of_id"`
	Note         string   `json:"note"`
	AdvanceDue   bool     `json:"advance_due"`
}

func (r expenseRequest) input() expenses.ExpenseInput {
	d := r.Date.Time
	if d.IsZero() {
		d = time.Now()
	}
	return expenses.ExpenseInput{
		Date: d, Provider: strings.TrimSpace(r.Provider), ProviderUUID: r.ProviderUUID, ItemID: r.ItemID, Kind: r.Kind,
		OrigAmount: money.FromMajor(r.OrigAmount), Currency: r.OrigCurrency, FxRate: r.FxRate,
		FeePercent: r.FeePercent, SharePercent: r.SharePercent, RefundOfID: r.RefundOfID, Note: r.Note,
		AdvanceDue: r.AdvanceDue,
	}
}

func (h *Handlers) CreateExpense(c *fiber.Ctx) error {
	var req expenseRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	e, err := h.Expenses.Create(c.UserContext(), req.input())
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": e.ID, "rub_amount": money.ToMajor(e.RubAmount)})
}

func (h *Handlers) UpdateExpense(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var req expenseRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	if _, err := h.Expenses.Update(c.UserContext(), id, req.input()); err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) DeleteExpense(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	if _, err := h.DB.Expense.Update().Where(expense.RefundOfID(id)).ClearRefundOfID().Save(ctx); err != nil {
		return err
	}
	err = h.DB.Expense.DeleteOneID(id).Exec(ctx)
	audit.Log(ctx, h.DB, "expense.delete", "expense", id, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) ProviderReport(c *fiber.Ctx) error {
	rows, err := h.Expenses.ProviderReport(c.UserContext())
	if err != nil {
		return err
	}
	out := make([]fiber.Map, 0, len(rows))
	for _, r := range rows {
		out = append(out, fiber.Map{
			"provider": r.Provider, "gross": money.ToMajor(r.Gross), "refunded": money.ToMajor(r.Refunded),
			"net": money.ToMajor(r.Net), "count": r.Count, "last": r.Last,
		})
	}
	return c.JSON(out)
}

type ExpenseItemView struct {
	ID           int            `json:"id"`
	Name         string         `json:"name"`
	Provider     string         `json:"provider"`
	ProviderUUID string         `json:"provider_uuid"`
	Currency     string         `json:"currency"`
	Pricing      string         `json:"pricing"`
	Amount       float64        `json:"amount"`
	Period       string         `json:"period"`
	FeePercent   float64        `json:"fee_percent"`
	SharePercent float64        `json:"share_percent"`
	PricePerGB   float64        `json:"price_per_gb"`
	MinCharge    float64        `json:"min_charge"`
	GBUnit       string         `json:"gb_unit"`
	MinMode      string         `json:"min_mode"`
	FreeGB       float64        `json:"free_gb"`
	Tiers        []tierView     `json:"tiers"`
	BillingDay   int            `json:"billing_day"`
	RwNodeUUID   string         `json:"rw_node_uuid"`
	RwSquadUUID  string         `json:"rw_squad_uuid"`
	NextDueDate  *time.Time     `json:"next_due_date"`
	Active       bool           `json:"active"`
	Notes        string         `json:"notes"`
	MonthlyRub   *float64       `json:"monthly_rub"`
	Rate         *float64       `json:"rate"`
	PlanError    string         `json:"plan_error,omitempty"`
	PlanErrCode  string         `json:"plan_error_code,omitempty"`
	PlanErrPar   map[string]any `json:"plan_error_params,omitempty"`
}

// tierView is a metered.Tier with the price in major units.
type tierView struct {
	UpToGB     float64 `json:"up_to_gb"`
	PricePerGB float64 `json:"price_per_gb"`
}

func tierViews(ts []metered.Tier) []tierView {
	out := make([]tierView, 0, len(ts))
	for _, t := range ts {
		out = append(out, tierView{UpToGB: t.UpToGB, PricePerGB: money.ToMajor(t.PricePerGB)})
	}
	return out
}

func expenseItemView(it *ent.ExpenseItem) ExpenseItemView {
	return ExpenseItemView{
		ID: it.ID, Name: it.Name, Provider: it.Provider, ProviderUUID: it.RwProviderUUID, Currency: it.Currency, Pricing: it.Pricing.String(),
		Amount: money.ToMajor(it.Amount), Period: it.Period.String(), FeePercent: it.FeePercent,
		SharePercent: it.SharePercent, PricePerGB: money.ToMajor(it.PricePerGB), MinCharge: money.ToMajor(it.MinCharge),
		GBUnit: it.GBUnit.String(), MinMode: it.MinMode.String(), FreeGB: it.FreeGB, Tiers: tierViews(it.Tiers), BillingDay: it.BillingDay,
		RwNodeUUID: it.RwNodeUUID, RwSquadUUID: it.RwSquadUUID, NextDueDate: it.NextDueDate, Active: it.Active, Notes: it.Notes,
	}
}

func (h *Handlers) ListExpenseItems(c *fiber.Ctx) error {
	ctx := c.UserContext()
	items, err := h.DB.ExpenseItem.Query().Order(ent.Desc(expenseitem.FieldActive), ent.Asc(expenseitem.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	planned, total, err := h.Expenses.Planned(ctx)
	if err != nil {
		return err
	}
	byID := map[int]expenses.PlannedItem{}
	for _, p := range planned {
		byID[p.ID] = p
	}
	out := make([]ExpenseItemView, 0, len(items))
	for _, it := range items {
		v := expenseItemView(it)
		if p, ok := byID[it.ID]; ok {
			if p.Error == "" {
				m, r := money.ToMajor(p.MonthlyRub), p.Rate
				v.MonthlyRub, v.Rate = &m, &r
			}
			v.PlanError = p.Error
			v.PlanErrCode, v.PlanErrPar = apperr.Fields(p.Err)
		}
		out = append(out, v)
	}
	return c.JSON(fiber.Map{"items": out, "monthly_total": money.ToMajor(total)})
}

type expenseItemRequest struct {
	Name         string     `json:"name"`
	Provider     string     `json:"provider"`
	ProviderUUID string     `json:"provider_uuid"`
	Currency     string     `json:"currency"`
	Pricing      string     `json:"pricing"`
	Amount       float64    `json:"amount"`
	Period       string     `json:"period"`
	FeePercent   float64    `json:"fee_percent"`
	SharePercent float64    `json:"share_percent"`
	PricePerGB   float64    `json:"price_per_gb"`
	MinCharge    float64    `json:"min_charge"`
	GBUnit       string     `json:"gb_unit"`
	MinMode      string     `json:"min_mode"`
	FreeGB       float64    `json:"free_gb"`
	Tiers        []tierView `json:"tiers"`
	BillingDay   int        `json:"billing_day"`
	RwNodeUUID   string     `json:"rw_node_uuid"`
	RwSquadUUID  string     `json:"rw_squad_uuid"`
	NextDueDate  Date       `json:"next_due_date"`
	Active       bool       `json:"active"`
	Notes        string     `json:"notes"`
}

func (r *expenseItemRequest) normalize(base string) {
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if r.Currency == "" {
		r.Currency = base
	}
	if r.Pricing == "" {
		r.Pricing = "fixed"
	}
	if r.Period == "" {
		r.Period = "month"
	}
	if r.SharePercent <= 0 {
		r.SharePercent = 100
	}
	if r.GBUnit == "" {
		r.GBUnit = string(metered.Binary)
	}
	if r.MinMode == "" {
		r.MinMode = string(metered.Floor)
	}
	if r.BillingDay == 0 {
		r.BillingDay = 1
	}
	if r.Pricing != "metered" {
		r.RwSquadUUID = ""
		r.Tiers = nil
	}
}

// tiers validates and converts the request's tiers: bounds must be
// positive and distinct, with at most one unlimited (0) step.
func (r *expenseItemRequest) tiers() ([]metered.Tier, error) {
	if len(r.Tiers) == 0 {
		return nil, nil
	}
	out := make([]metered.Tier, 0, len(r.Tiers))
	seen := map[float64]bool{}
	for _, t := range r.Tiers {
		if t.UpToGB < 0 || t.PricePerGB < 0 {
			return nil, errors.New("tiers: negative value")
		}
		if seen[t.UpToGB] {
			return nil, errors.New("tiers: duplicate bound")
		}
		seen[t.UpToGB] = true
		out = append(out, metered.Tier{UpToGB: t.UpToGB, PricePerGB: money.FromMajor(t.PricePerGB)})
	}
	return metered.NormalizeTiers(out), nil
}

func (h *Handlers) CreateExpenseItem(c *fiber.Ctx) error {
	var r expenseItemRequest
	if err := bind(c, &r); err != nil {
		return err
	}
	r.normalize(h.FX.Base(c.UserContext()))
	tiers, err := r.tiers()
	if err != nil {
		return badRequest(err)
	}
	it, err := h.DB.ExpenseItem.Create().
		SetName(r.Name).SetProvider(r.Provider).SetRwProviderUUID(r.ProviderUUID).SetCurrency(r.Currency).
		SetPricing(expenseitem.Pricing(r.Pricing)).SetAmount(money.FromMajor(r.Amount)).
		SetPeriod(expenseitem.Period(r.Period)).SetFeePercent(r.FeePercent).SetSharePercent(r.SharePercent).
		SetPricePerGB(money.FromMajor(r.PricePerGB)).SetMinCharge(money.FromMajor(r.MinCharge)).
		SetGBUnit(expenseitem.GBUnit(r.GBUnit)).SetMinMode(expenseitem.MinMode(r.MinMode)).
		SetFreeGB(r.FreeGB).SetTiers(tiers).SetBillingDay(r.BillingDay).
		SetRwNodeUUID(r.RwNodeUUID).SetRwSquadUUID(r.RwSquadUUID).SetNillableNextDueDate(r.NextDueDate.Ptr()).
		SetActive(r.Active).SetNotes(r.Notes).
		Save(c.UserContext())
	if err != nil {
		return badRequest(err)
	}
	audit.Log(c.UserContext(), h.DB, "expense_item.create", "expense_item", it.ID, r, nil)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": it.ID})
}

func (h *Handlers) UpdateExpenseItem(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var r expenseItemRequest
	if err := bind(c, &r); err != nil {
		return err
	}
	r.normalize(h.FX.Base(c.UserContext()))
	tiers, err := r.tiers()
	if err != nil {
		return badRequest(err)
	}
	q := h.DB.ExpenseItem.UpdateOneID(id).
		SetName(r.Name).SetProvider(r.Provider).SetRwProviderUUID(r.ProviderUUID).SetCurrency(r.Currency).
		SetPricing(expenseitem.Pricing(r.Pricing)).SetAmount(money.FromMajor(r.Amount)).
		SetPeriod(expenseitem.Period(r.Period)).SetFeePercent(r.FeePercent).SetSharePercent(r.SharePercent).
		SetPricePerGB(money.FromMajor(r.PricePerGB)).SetMinCharge(money.FromMajor(r.MinCharge)).
		SetGBUnit(expenseitem.GBUnit(r.GBUnit)).SetMinMode(expenseitem.MinMode(r.MinMode)).
		SetFreeGB(r.FreeGB).SetTiers(tiers).SetBillingDay(r.BillingDay).
		SetRwNodeUUID(r.RwNodeUUID).SetRwSquadUUID(r.RwSquadUUID).SetActive(r.Active).SetNotes(r.Notes)
	if d := r.NextDueDate.Ptr(); d != nil {
		q.SetNextDueDate(*d)
	} else {
		q.ClearNextDueDate()
	}
	err = q.Exec(c.UserContext())
	audit.Log(c.UserContext(), h.DB, "expense_item.update", "expense_item", id, r, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) DeleteExpenseItem(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	if _, err := h.DB.Expense.Update().Where(expense.ExpenseItemID(id)).ClearExpenseItemID().Save(ctx); err != nil {
		return err
	}
	err = h.DB.ExpenseItem.DeleteOneID(id).Exec(ctx)
	audit.Log(ctx, h.DB, "expense_item.delete", "expense_item", id, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// MeteredSummary: GET /expense-items/:id/metered?month=YYYY-MM.
func (h *Handlers) MeteredSummary(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	it, err := h.DB.ExpenseItem.Get(ctx, id)
	if err != nil {
		return badRequest(err)
	}
	t := time.Now().In(h.Config.Location)
	if m := c.Query("month"); m != "" {
		if t, err = expenses.PeriodStart(m, it.BillingDay, h.Config.Location); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "month: YYYY-MM")
		}
	}
	sum, err := h.Expenses.MeteredSummary(ctx, it, t)
	if err != nil {
		return badRequest(err)
	}
	h.Expenses.WithConsumers(ctx, sum, 20)
	return c.JSON(meteredView(sum))
}

func meteredView(s *expenses.MeteredSummary) fiber.Map {
	consumers := make([]fiber.Map, 0, len(s.TopConsumers))
	for _, c := range s.TopConsumers {
		consumers = append(consumers, fiber.Map{
			"rw_user_id": c.RwUserID, "username": c.Username, "customer_id": c.CustomerID,
			"customer_name": c.CustomerName, "subscription_id": c.SubscriptionID, "sub_title": c.SubTitle,
			"gb": c.GB, "share_percent": c.SharePercent,
			"cost_rub": money.ToMajor(c.CostRub),
		})
	}
	return fiber.Map{
		"item_id": s.ItemID, "name": s.Name, "node_uuid": s.NodeUUID, "node_name": s.NodeName,
		"squad_uuid": s.SquadUUID, "squad_share_percent": s.SquadSharePct,
		"period": s.Period, "period_start": s.PeriodStart, "period_end": s.PeriodEnd, "currency": s.Currency,
		"gb_unit": s.GBUnit, "min_mode": s.MinMode, "free_gb": s.FreeGB, "tiers": tierViews(s.Tiers),
		"price_per_gb": money.ToMajor(s.PricePerGB), "min_charge": money.ToMajor(s.MinCharge),
		"included_gb": s.IncludedGB, "used_gb": s.UsedGB,
		"cost": money.ToMajor(s.Cost), "cost_rub": money.ToMajor(s.CostRub),
		"forecast_gb": s.ForecastGB, "forecast_cost": money.ToMajor(s.ForecastCost),
		"forecast_rub": money.ToMajor(s.ForecastRub), "daily": s.Daily,
		"top_consumers": consumers, "consumer_error": s.ConsumerError,
	}
}

type closePeriodRequest struct {
	Period string `json:"period"`
}

func (h *Handlers) ClosePeriod(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var req closePeriodRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	e, err := h.Expenses.ClosePeriod(c.UserContext(), id, req.Period)
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": e.ID, "rub_amount": money.ToMajor(e.RubAmount)})
}

func (h *Handlers) SyncTraffic(c *fiber.Ctx) error {
	return h.runJob(c, "traffic")
}

// FxRate: GET /fx/rate?currency=EUR&date=2026-10-01.
func (h *Handlers) FxRate(c *fiber.Ctx) error {
	d := time.Now()
	if s := c.Query("date"); s != "" {
		var err error
		if d, err = time.ParseInLocation("2006-01-02", s[:min(10, len(s))], h.Config.Location); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "date: YYYY-MM-DD")
		}
	}
	rate, err := h.FX.Rate(c.UserContext(), c.Query("currency", h.FX.Base(c.UserContext())), d)
	if err != nil {
		return badRequest(err)
	}
	return c.JSON(fiber.Map{"rate": rate})
}
