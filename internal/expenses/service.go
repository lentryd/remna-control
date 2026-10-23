package expenses

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"remna-control/ent"
	"remna-control/ent/expense"
	"remna-control/ent/expenseitem"
	"remna-control/ent/rwuser"
	"remna-control/ent/trafficsnapshot"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/fx"
	"remna-control/internal/jobs"
	"remna-control/internal/metered"
	"remna-control/internal/remnawave"
)

type Service struct {
	db  *ent.Client
	rw  *remnawave.Client
	fx  *fx.Service
	loc *time.Location

	mu       sync.Mutex
	topCache map[string]topCacheEntry
}

type topCacheEntry struct {
	at    time.Time
	users []remnawave.UserUsage
}

func New(db *ent.Client, rw *remnawave.Client, fxs *fx.Service, loc *time.Location) *Service {
	return &Service{db: db, rw: rw, fx: fxs, loc: loc, topCache: map[string]topCacheEntry{}}
}

// ExpenseInput creates or updates an expense. Amounts are minor units.
type ExpenseInput struct {
	Date         time.Time
	Provider     string
	ProviderUUID string // panel infra provider, optional
	ItemID       *int
	Kind         string // charge | refund
	OrigAmount   int64
	Currency     string
	FxRate       *float64 // nil = the published rate on Date
	FeePercent   float64
	SharePercent float64
	RefundOfID   *int
	Note         string
	// AdvanceDue moves the item's next payment date one period on: the
	// expense pays for it (new charges only).
	AdvanceDue bool
}

func (s *Service) prepare(ctx context.Context, in *ExpenseInput) (int64, float64, error) {
	if in.OrigAmount <= 0 {
		return 0, 0, apperr.New("amount_positive", "amount must be greater than zero")
	}
	if in.Kind == "" {
		in.Kind = "charge"
	}
	base := s.fx.Base(ctx)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = base
	}
	if in.SharePercent <= 0 {
		in.SharePercent = 100
	}
	rate := 1.0
	if in.FxRate != nil && *in.FxRate > 0 {
		rate = *in.FxRate
	} else if in.Currency != base {
		r, err := s.fx.Rate(ctx, in.Currency, in.Date)
		if err != nil {
			return 0, 0, apperr.Wrap(err, "fx.rate_failed", "{{currency}} rate on {{date}}: {{error}}", "currency", in.Currency, "date", in.Date.Format("2006-01-02"))
		}
		rate = r
	}
	rub := ToBase(in.OrigAmount, rate, in.FeePercent, in.SharePercent)
	if in.Kind == "refund" {
		rub = -rub
	}
	return rub, rate, nil
}

func (s *Service) Create(ctx context.Context, in ExpenseInput) (*ent.Expense, error) {
	rub, rate, err := s.prepare(ctx, &in)
	if err != nil {
		return nil, err
	}
	if in.RefundOfID != nil && in.Provider == "" {
		if orig, err := s.db.Expense.Get(ctx, *in.RefundOfID); err == nil {
			in.Provider, in.ProviderUUID = orig.Provider, orig.RwProviderUUID
		}
	}
	e, err := s.db.Expense.Create().
		SetDate(in.Date).SetProvider(in.Provider).SetRwProviderUUID(in.ProviderUUID).SetNillableExpenseItemID(in.ItemID).
		SetKind(expense.Kind(in.Kind)).SetOrigAmount(in.OrigAmount).SetOrigCurrency(in.Currency).
		SetFxRate(rate).SetFeePercent(in.FeePercent).SetSharePercent(in.SharePercent).
		SetRubAmount(rub).SetNillableRefundOfID(in.RefundOfID).SetNote(in.Note).
		Save(ctx)
	audit.Log(ctx, s.db, "expense.create", "expense", idOf(e), in, err)
	if err != nil {
		return nil, err
	}
	if in.AdvanceDue && in.ItemID != nil && in.Kind == "charge" {
		if err := s.advanceDue(ctx, *in.ItemID); err != nil {
			return e, err
		}
	}
	return e, nil
}

// advanceDue moves an item's next payment date one period on; items
// without a date (or dated by the panel) are left alone.
func (s *Service) advanceDue(ctx context.Context, itemID int) error {
	it, err := s.db.ExpenseItem.Get(ctx, itemID)
	if err != nil || it.NextDueDate == nil {
		return err
	}
	next := it.NextDueDate.AddDate(0, 1, 0)
	if it.Period == expenseitem.PeriodYear {
		next = it.NextDueDate.AddDate(1, 0, 0)
	}
	return s.db.ExpenseItem.UpdateOneID(itemID).SetNextDueDate(next).Exec(ctx)
}

func (s *Service) Update(ctx context.Context, id int, in ExpenseInput) (*ent.Expense, error) {
	rub, rate, err := s.prepare(ctx, &in)
	if err != nil {
		return nil, err
	}
	q := s.db.Expense.UpdateOneID(id).
		SetDate(in.Date).SetProvider(in.Provider).SetRwProviderUUID(in.ProviderUUID).
		SetKind(expense.Kind(in.Kind)).SetOrigAmount(in.OrigAmount).SetOrigCurrency(in.Currency).
		SetFxRate(rate).SetFeePercent(in.FeePercent).SetSharePercent(in.SharePercent).
		SetRubAmount(rub).SetNote(in.Note)
	if in.ItemID != nil {
		q.SetExpenseItemID(*in.ItemID)
	} else {
		q.ClearExpenseItemID()
	}
	if in.RefundOfID != nil {
		q.SetRefundOfID(*in.RefundOfID)
	} else {
		q.ClearRefundOfID()
	}
	e, err := q.Save(ctx)
	audit.Log(ctx, s.db, "expense.update", "expense", id, in, err)
	return e, err
}

func idOf(e *ent.Expense) int {
	if e == nil {
		return 0
	}
	return e.ID
}

// ProviderTotals is the per-provider report: gross spend is kept apart from
// refunds so "how much went out before the refund" is never lost.
type ProviderTotals struct {
	Provider     string    `json:"provider"`
	ProviderUUID string    `json:"provider_uuid"`
	Gross        int64     `json:"gross"`
	Refunded     int64     `json:"refunded"`
	Net          int64     `json:"net"`
	Count        int       `json:"count"`
	Last         time.Time `json:"last"`
}

func (s *Service) ProviderReport(ctx context.Context) ([]ProviderTotals, error) {
	rows, err := s.db.Expense.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	by := map[string]*ProviderTotals{}
	for _, e := range rows {
		name := e.Provider
		if name == "" {
			name = "—"
		}
		p := by[name]
		if p == nil {
			p = &ProviderTotals{Provider: name}
			by[name] = p
		}
		if e.RwProviderUUID != "" {
			p.ProviderUUID = e.RwProviderUUID
		}
		if e.RubAmount >= 0 {
			p.Gross += e.RubAmount
		} else {
			p.Refunded += -e.RubAmount
		}
		p.Net += e.RubAmount
		p.Count++
		if e.Date.After(p.Last) {
			p.Last = e.Date
		}
	}
	out := make([]ProviderTotals, 0, len(by))
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Gross > out[j].Gross })
	return out, nil
}

// PlannedItem is one recurring cost converted to RUB per month at today's
// rate (metered items use this month's forecast).
type PlannedItem struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	Provider    string     `json:"provider"`
	Pricing     string     `json:"pricing"`
	Currency    string     `json:"currency"`
	Rate        float64    `json:"rate"`
	MonthlyRub  int64      `json:"monthly_rub"`
	NextDueDate *time.Time `json:"next_due_date"`
	Error       string     `json:"error,omitempty"`
	Err         error      `json:"-"`
}

func (s *Service) Planned(ctx context.Context) ([]PlannedItem, int64, error) {
	items, err := s.db.ExpenseItem.Query().Where(expenseitem.Active(true)).Order(ent.Asc(expenseitem.FieldID)).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now().In(s.loc)
	var total int64
	out := make([]PlannedItem, 0, len(items))
	for _, it := range items {
		p := PlannedItem{ID: it.ID, Name: it.Name, Provider: it.Provider, Pricing: it.Pricing.String(), Currency: it.Currency, NextDueDate: it.NextDueDate}
		rate, err := s.fx.Rate(ctx, it.Currency, now)
		if err != nil {
			p.Error, p.Err = err.Error(), err
			out = append(out, p)
			continue
		}
		p.Rate = rate
		var orig int64
		if it.Pricing == expenseitem.PricingMetered {
			sum, err := s.MeteredSummary(ctx, it, now)
			if err != nil {
				p.Error, p.Err = err.Error(), err
			} else {
				orig = sum.ForecastCost
			}
		} else {
			orig = it.Amount
			if it.Period == expenseitem.PeriodYear {
				orig /= 12
			}
		}
		p.MonthlyRub = ToBase(orig, rate, it.FeePercent, it.SharePercent)
		total += p.MonthlyRub
		out = append(out, p)
	}
	return out, total, nil
}

// SyncTraffic stores per-node daily traffic between from and to.
func (s *Service) SyncTraffic(ctx context.Context, from, to time.Time) error {
	usage, err := s.rw.NodesUsageByRange(ctx, from, to)
	if err != nil {
		return err
	}
	days := dayList(from, to)
	for _, series := range usage.Series {
		for i, v := range series.Data {
			day := dayFor(usage.Categories, days, i)
			if day == "" {
				continue
			}
			if err := s.db.TrafficSnapshot.Create().
				SetDate(day).SetNodeUUID(series.UUID).SetNodeName(series.Name).
				SetBytes(int64(v)).SetSyncedAt(time.Now()).
				OnConflictColumns(trafficsnapshot.FieldDate, trafficsnapshot.FieldNodeUUID).
				UpdateNewValues().Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// dayFor maps a series index to a date: the category itself when it's an
// ISO date, otherwise the index-th day of the requested range.
func dayFor(categories, days []string, i int) string {
	if i < len(categories) {
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(categories[i])); err == nil {
			return t.Format("2006-01-02")
		}
		if len(categories) >= 10 {
			if t, err := time.Parse("2006-01-02", categories[i][:10]); err == nil {
				return t.Format("2006-01-02")
			}
		}
	}
	if i < len(days) {
		return days[i]
	}
	return ""
}

func dayList(from, to time.Time) []string {
	var out []string
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}

// SyncFrom is where a routine traffic sync starts: early enough to cover
// the current and previous period of any billing day.
func SyncFrom(now time.Time) time.Time {
	start, _ := MonthBounds(now.AddDate(0, -2, 0))
	return start
}

// TrafficJob refreshes the current and previous periods' node traffic. It
// rewrites the same rows every time, so it never reports a change.
func (s *Service) TrafficJob(ctx context.Context) (jobs.Result, error) {
	now := time.Now().In(s.loc)
	if err := s.SyncTraffic(ctx, SyncFrom(now), now); err != nil {
		return jobs.Result{}, err
	}
	return jobs.Result{Summary: "traffic synced"}, nil
}

// DayTraffic is one day of a node's traffic.
type DayTraffic struct {
	Date string  `json:"date"`
	GB   float64 `json:"gb"`
}

// Consumer is one user's share of a metered node's traffic.
type Consumer struct {
	RwUserID     int    `json:"rw_user_id"`
	Username     string `json:"username"`
	CustomerID   *int   `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	// SubscriptionID/SubTitle tell a customer's subscriptions apart: the
	// subscription label (or panel username), plus the add-on name.
	SubscriptionID *int    `json:"subscription_id"`
	SubTitle       string  `json:"sub_title"`
	GB             float64 `json:"gb"`
	SharePercent   float64 `json:"share_percent"`
	CostRub        int64   `json:"cost_rub"`
}

// MeteredSummary is the state of a metered item for the period containing t.
type MeteredSummary struct {
	ItemID        int            `json:"item_id"`
	Name          string         `json:"name"`
	NodeUUID      string         `json:"node_uuid"`
	NodeName      string         `json:"node_name"`
	SquadUUID     string         `json:"squad_uuid"`
	SquadSharePct float64        `json:"squad_share_percent"`
	Period        string         `json:"period"`
	PeriodStart   time.Time      `json:"period_start"`
	PeriodEnd     time.Time      `json:"period_end"`
	Currency      string         `json:"currency"`
	GBUnit        string         `json:"gb_unit"`
	MinMode       string         `json:"min_mode"`
	FreeGB        float64        `json:"free_gb"`
	Tiers         []metered.Tier `json:"tiers"`
	PricePerGB    int64          `json:"price_per_gb"`
	MinCharge     int64          `json:"min_charge"`
	IncludedGB    float64        `json:"included_gb"`
	UsedGB        float64        `json:"used_gb"`
	Cost          int64          `json:"cost"`
	CostRub       int64          `json:"cost_rub"`
	ForecastGB    float64        `json:"forecast_gb"`
	ForecastCost  int64          `json:"forecast_cost"`
	ForecastRub   int64          `json:"forecast_rub"`
	Daily         []DayTraffic   `json:"daily"`
	TopConsumers  []Consumer     `json:"top_consumers,omitempty"`
	ConsumerError string         `json:"consumer_error,omitempty"`
}

// MeteredSummary computes usage, cost and forecast of a metered item for
// the billing period containing t, from stored snapshots.
func (s *Service) MeteredSummary(ctx context.Context, it *ent.ExpenseItem, t time.Time) (*MeteredSummary, error) {
	if it.RwNodeUUID == "" {
		return nil, apperr.New("expense.no_node", "expense item {{name}} has no node selected", "name", it.Name)
	}
	start, end := PeriodBounds(t.In(s.loc), it.BillingDay)
	now := time.Now().In(s.loc)
	last := end
	current := !now.Before(start) && !now.After(end.AddDate(0, 0, 1))
	if current {
		last = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	}

	snaps, err := s.db.TrafficSnapshot.Query().
		Where(trafficsnapshot.NodeUUID(it.RwNodeUUID),
			trafficsnapshot.DateGTE(start.Format("2006-01-02")),
			trafficsnapshot.DateLTE(last.Format("2006-01-02"))).
		All(ctx)
	if err != nil {
		return nil, err
	}
	byDay := make(map[string]int64, len(snaps))
	nodeName := ""
	for _, sn := range snaps {
		byDay[sn.Date] = sn.Bytes
		nodeName = sn.NodeName
	}

	pricing := PricingOf(it)
	unit := pricing.Unit
	sum := &MeteredSummary{
		ItemID: it.ID, Name: it.Name, NodeUUID: it.RwNodeUUID, NodeName: nodeName,
		SquadUUID: it.RwSquadUUID,
		Period:    start.Format("2006-01"), PeriodStart: start, PeriodEnd: end, Currency: it.Currency,
		GBUnit: it.GBUnit.String(), MinMode: it.MinMode.String(), FreeGB: it.FreeGB, Tiers: it.Tiers,
		PricePerGB: it.PricePerGB, MinCharge: it.MinCharge,
		IncludedGB: pricing.IncludedGB(),
	}
	// With a squad, the node's daily traffic is scaled by the squad's share
	// of it over the month: the panel gives per-user totals, not per-day.
	ratio := 1.0
	if it.RwSquadUUID != "" {
		users, err := s.nodeUsers(ctx, it.RwNodeUUID, start, end)
		if err != nil {
			return nil, apperr.Wrap(err, "expense.node_users_failed", "node users traffic: {{error}}")
		}
		members, err := s.squadMembers(ctx, it.RwSquadUUID)
		if err != nil {
			return nil, err
		}
		var all, squad float64
		for _, u := range users {
			all += u.TotalBytes
			if members[u.ID] {
				squad += u.TotalBytes
			}
		}
		ratio = 0
		if all > 0 {
			ratio = squad / all
		}
		sum.SquadSharePct = round2(ratio * 100)
	}
	var daily []int64
	var used int64
	for _, d := range dayList(start, last) {
		b := int64(float64(byDay[d]) * ratio)
		daily = append(daily, b)
		used += b
		sum.Daily = append(sum.Daily, DayTraffic{Date: d, GB: round2(metered.GB(b, unit))})
	}
	forecast := used
	if current {
		forecast = Forecast(daily, now, end)
	}
	rate, err := s.fx.Rate(ctx, it.Currency, now)
	if err != nil {
		return nil, err
	}
	sum.UsedGB = round2(metered.GB(used, unit))
	sum.Cost = pricing.Cost(used)
	sum.CostRub = ToBase(sum.Cost, rate, it.FeePercent, it.SharePercent)
	sum.ForecastGB = round2(metered.GB(forecast, unit))
	sum.ForecastCost = pricing.Cost(forecast)
	sum.ForecastRub = ToBase(sum.ForecastCost, rate, it.FeePercent, it.SharePercent)
	return sum, nil
}

// WithConsumers adds the top users of the node over the summary's period,
// with their share of the (forecast) cost.
func (s *Service) WithConsumers(ctx context.Context, sum *MeteredSummary, limit int) {
	start, end := sum.PeriodStart, sum.PeriodEnd
	users, err := s.nodeUsers(ctx, sum.NodeUUID, start, end)
	if err != nil {
		sum.ConsumerError = err.Error()
		return
	}
	if sum.SquadUUID != "" {
		members, err := s.squadMembers(ctx, sum.SquadUUID)
		if err != nil {
			sum.ConsumerError = err.Error()
			return
		}
		var in []remnawave.UserUsage
		for _, u := range users {
			if members[u.ID] {
				in = append(in, u)
			}
		}
		users = in
	} else {
		users = append([]remnawave.UserUsage(nil), users...)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].TotalBytes > users[j].TotalBytes })
	var total float64
	for _, u := range users {
		total += u.TotalBytes
	}
	if len(users) > limit {
		users = users[:limit]
	}
	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	cache, _ := s.db.RwUser.Query().Where(rwuser.IDIn(ids...)).
		WithSubscription(func(q *ent.SubscriptionQuery) { q.WithCustomer() }).
		WithSubscriptionAddon(func(q *ent.SubscriptionAddonQuery) {
			q.WithAddon().WithSubscription(func(q *ent.SubscriptionQuery) { q.WithCustomer().WithRwUser() })
		}).All(ctx)
	byID := make(map[int]*ent.RwUser, len(cache))
	for _, u := range cache {
		byID[u.ID] = u
	}
	for _, u := range users {
		c := Consumer{RwUserID: u.ID, GB: round2(u.TotalBytes / metered.Unit(sum.GBUnit).BytesPerGB())}
		if total > 0 {
			c.SharePercent = round2(u.TotalBytes / total * 100)
			c.CostRub = int64(float64(sum.ForecastRub) * u.TotalBytes / total)
		}
		if ru := byID[u.ID]; ru != nil {
			c.Username = ru.Username
			var sub *ent.Subscription
			subUsername, addonName := ru.Username, ""
			if ru.Edges.Subscription != nil {
				sub = ru.Edges.Subscription
			} else if sa := ru.Edges.SubscriptionAddon; sa != nil {
				sub = sa.Edges.Subscription
				if sa.Edges.Addon != nil {
					addonName = sa.Edges.Addon.Name
				}
				if sub != nil && sub.Edges.RwUser != nil {
					subUsername = sub.Edges.RwUser.Username
				}
			}
			if sub != nil {
				c.SubscriptionID = &sub.ID
				c.SubTitle = sub.Label
				if c.SubTitle == "" {
					c.SubTitle = subUsername
				}
				if addonName != "" {
					c.SubTitle = addonName + " · " + c.SubTitle
				}
				if sub.Edges.Customer != nil {
					c.CustomerID = &sub.Edges.Customer.ID
					c.CustomerName = sub.Edges.Customer.Name
				}
			}
		}
		sum.TopConsumers = append(sum.TopConsumers, c)
	}
}

// nodeUsers returns users' traffic on a node over [start, end], cached
// for 10 minutes. Callers must not modify the result.
func (s *Service) nodeUsers(ctx context.Context, node string, start, end time.Time) ([]remnawave.UserUsage, error) {
	key := node + "|" + start.Format("2006-01-02") + "|" + end.Format("2006-01-02")
	s.mu.Lock()
	cached, ok := s.topCache[key]
	s.mu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached.users, nil
	}
	res, err := s.rw.NodeUsersUsage(ctx, []string{node}, start, end)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.topCache[key] = topCacheEntry{at: time.Now(), users: res[node]}
	s.mu.Unlock()
	return res[node], nil
}

// squadMembers returns the ids of synced panel users in an internal squad.
func (s *Service) squadMembers(ctx context.Context, squad string) (map[int]bool, error) {
	users, err := s.db.RwUser.Query().Where(rwuser.Deleted(false)).Select(rwuser.FieldID, rwuser.FieldSquadUuids).All(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int]bool{}
	for _, u := range users {
		if slices.Contains(u.SquadUuids, squad) {
			out[u.ID] = true
		}
	}
	return out, nil
}

// ClosePeriod books a metered item's month as an expense (amount = the
// calculated cost; edit it to the provider's invoice later — the
// calculation stays in calc_rub_amount).
func (s *Service) ClosePeriod(ctx context.Context, itemID int, period string) (*ent.Expense, error) {
	it, err := s.db.ExpenseItem.Get(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if it.Pricing != expenseitem.PricingMetered {
		return nil, apperr.New("expense.not_metered", "the expense item isn't billed by traffic")
	}
	start, err := PeriodStart(period, it.BillingDay, s.loc)
	if err != nil {
		return nil, apperr.New("expense.bad_period", "period must be YYYY-MM")
	}
	exists, err := s.db.Expense.Query().Where(expense.ExpenseItemID(it.ID), expense.Period(period)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperr.Status(http.StatusConflict, "expense.period_closed", "period {{period}} is already closed", "period", period)
	}
	_, end := PeriodBounds(start, it.BillingDay)
	_ = s.SyncTraffic(ctx, start, end)
	sum, err := s.MeteredSummary(ctx, it, start)
	if err != nil {
		return nil, err
	}
	e, err := s.Create(ctx, ExpenseInput{
		Date: end, Provider: it.Provider, ProviderUUID: it.RwProviderUUID, ItemID: &it.ID, Kind: "charge",
		OrigAmount: sum.Cost, Currency: it.Currency, FeePercent: it.FeePercent, SharePercent: it.SharePercent,
		Note: fmt.Sprintf("%s %s: %.2f GB", it.Name, period, sum.UsedGB),
	})
	if err != nil {
		return nil, err
	}
	return s.db.Expense.UpdateOne(e).
		SetPeriod(period).SetMeteredGB(sum.UsedGB).SetCalcRubAmount(e.RubAmount).
		Save(ctx)
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
