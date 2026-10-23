package handlers

import (
	"context"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/expenseitem"
	"remna-control/internal/apperr"
	"remna-control/internal/money"
	"remna-control/internal/remnawave"
	"remna-control/internal/settings"
)

type ExpiringItem struct {
	Kind         string     `json:"kind"`
	ID           int        `json:"id"`
	SubID        int        `json:"subscription_id"`
	Title        string     `json:"title"`
	CustomerID   int        `json:"customer_id"`
	CustomerName string     `json:"customer_name"`
	Status       string     `json:"status"`
	ExpireAt     *time.Time `json:"expire_at"`
	DaysLeft     int        `json:"days_left"`
	Price        float64    `json:"price"`
	Balance      float64    `json:"balance"`
}

// TrafficItem is a panel user running out of its traffic limit.
type TrafficItem struct {
	Kind             string     `json:"kind"`
	ID               int        `json:"id"`
	SubID            int        `json:"subscription_id"`
	Title            string     `json:"title"`
	CustomerID       int        `json:"customer_id"`
	CustomerName     string     `json:"customer_name"`
	Status           string     `json:"status"`
	UsedBytes        int64      `json:"used_traffic_bytes"`
	LimitBytes       int64      `json:"traffic_limit_bytes"`
	Strategy         string     `json:"traffic_limit_strategy"`
	UsedPct          float64    `json:"used_pct"`
	NextResetAt      *time.Time `json:"next_traffic_reset_at"`
	LastTrafficReset *time.Time `json:"last_traffic_reset_at"`
}

// trafficLowPct is where a limited user counts as running out of traffic
// (web: TRAFFIC_LOW_PCT).
const trafficLowPct = 80

// trafficStats splits live panel users by how much of their limit is left.
type trafficStats struct {
	OK        int `json:"ok"`
	Low       int `json:"low"`
	Limited   int `json:"limited"`
	Unlimited int `json:"unlimited"`
}

type monthPoint struct {
	Month    string  `json:"month"`
	Income   float64 `json:"income"`
	Expenses float64 `json:"expenses"`
}

func (h *Handlers) Dashboard(c *fiber.Ctx) error {
	ctx := c.UserContext()
	now := time.Now().In(h.Config.Location)
	window := h.Settings.Int(ctx, settings.ExpiringWindowDays)
	horizon := now.AddDate(0, 0, window)

	subs, err := h.loadSubscriptions(ctx)
	if err != nil {
		return err
	}
	balances, err := h.Billing.Balances(ctx)
	if err != nil {
		return err
	}

	var mrr float64
	activeSubs, activeAddons := 0, 0
	customers := map[int]bool{}
	var expiring []ExpiringItem
	trafficLow := []TrafficItem{}
	var traffic trafficStats
	addTraffic := func(kind string, id int, title string, s SubscriptionView, u *RwUserView) {
		if u == nil || u.Deleted || (u.Status != "ACTIVE" && u.Status != "LIMITED") {
			return
		}
		if u.TrafficLimitBytes <= 0 {
			traffic.Unlimited++
			return
		}
		pct := float64(u.UsedTrafficBytes) * 100 / float64(u.TrafficLimitBytes)
		switch {
		case u.Status == "LIMITED":
			traffic.Limited++
		case pct >= trafficLowPct:
			traffic.Low++
		default:
			traffic.OK++
			return
		}
		trafficLow = append(trafficLow, TrafficItem{
			Kind: kind, ID: id, SubID: s.ID, Title: title, CustomerID: s.CustomerID, CustomerName: s.CustomerName,
			Status: u.Status, UsedBytes: u.UsedTrafficBytes, LimitBytes: u.TrafficLimitBytes, Strategy: u.TrafficLimitStrategy,
			UsedPct: pct, NextResetAt: u.NextTrafficResetAt, LastTrafficReset: u.LastTrafficResetAt,
		})
	}
	add := func(kind string, id int, title string, s SubscriptionView, u *RwUserView, price float64) {
		if u == nil || u.Deleted || u.ExpireAt == nil || u.Status == "DISABLED" {
			return
		}
		exp := *u.ExpireAt
		// Long-expired users are churned, not "expiring".
		if exp.After(horizon) || exp.Before(now.AddDate(0, 0, -30)) {
			return
		}
		expiring = append(expiring, ExpiringItem{
			Kind: kind, ID: id, SubID: s.ID, Title: title, CustomerID: s.CustomerID, CustomerName: s.CustomerName,
			Status: u.Status, ExpireAt: u.ExpireAt, DaysLeft: int(exp.Sub(now).Hours() / 24), Price: price,
			Balance: money.ToMajor(balances[s.CustomerID]),
		})
	}
	for _, s := range subs {
		if s.CustomerArch {
			continue
		}
		mrr += monthlyLive(s)
		if live(s.RwUser) {
			activeSubs++
			customers[s.CustomerID] = true
		}
		add("subscription", s.ID, s.Title, s, s.RwUser, s.Price)
		addTraffic("subscription", s.ID, s.Title, s, s.RwUser)
		for _, a := range s.Addons {
			if a.RwUser != nil && live(a.RwUser) {
				activeAddons++
			}
			addTraffic("addon", a.ID, a.AddonName+" · "+s.Title, s, a.RwUser)
			if !a.Included { // included add-ons expire with their subscription
				add("addon", a.ID, a.AddonName+" · "+s.Title, s, a.RwUser, a.Price)
			}
		}
	}
	sort.Slice(expiring, func(i, j int) bool { return expiring[i].ExpireAt.Before(*expiring[j].ExpireAt) })
	sort.Slice(trafficLow, func(i, j int) bool { return trafficLow[i].UsedPct > trafficLow[j].UsedPct })

	planned, plannedTotal, err := h.Expenses.Planned(ctx)
	if err != nil {
		return err
	}
	// Upcoming infrastructure payments: an item's own date wins, otherwise
	// the panel's Infra Billing date for its node; billing nodes not tied to
	// any item are listed too.
	allItems, err := h.DB.ExpenseItem.Query().Where(expenseitem.Active(true)).All(ctx)
	if err != nil {
		return err
	}
	itemByID := map[int]*ent.ExpenseItem{}
	for _, it := range allItems {
		itemByID[it.ID] = it
	}
	panelDue := map[string]time.Time{}
	var billingNodes []remnawave.InfraBillingNode
	providerName := map[string]string{}
	bctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	if bn, err := h.RW.InfraBillingNodes(bctx); err == nil {
		billingNodes = bn
		for _, b := range bn {
			if b.NodeUUID != nil {
				panelDue[*b.NodeUUID] = b.NextBillingAt
			}
		}
		if ps, err := h.RW.InfraProviders(bctx); err == nil {
			for _, p := range ps {
				providerName[p.UUID] = p.Name
			}
		}
	}
	cancel()
	dueSoon := []fiber.Map{}
	usedNodes := map[string]bool{}
	for _, p := range planned {
		it := itemByID[p.ID]
		due, source := p.NextDueDate, "item"
		if it != nil && it.RwNodeUUID != "" {
			usedNodes[it.RwNodeUUID] = true
			if due == nil {
				if d, ok := panelDue[it.RwNodeUUID]; ok {
					due, source = &d, "panel"
				}
			}
		}
		if due != nil && due.Before(horizon) {
			m := fiber.Map{"id": p.ID, "name": p.Name, "provider": p.Provider, "next_due_date": due, "monthly_rub": money.ToMajor(p.MonthlyRub), "source": source}
			if it != nil {
				m["provider_uuid"], m["node_uuid"] = it.RwProviderUUID, it.RwNodeUUID
			}
			dueSoon = append(dueSoon, m)
		}
	}
	for _, b := range billingNodes {
		if (b.NodeUUID != nil && usedNodes[*b.NodeUUID]) || !b.NextBillingAt.Before(horizon) {
			continue
		}
		name := providerName[b.ProviderUUID]
		if b.Name != nil && *b.Name != "" {
			name = *b.Name
		}
		due := b.NextBillingAt
		m := fiber.Map{"id": 0, "name": name, "provider": providerName[b.ProviderUUID], "provider_uuid": b.ProviderUUID,
			"next_due_date": &due, "source": "panel"}
		if b.NodeUUID != nil {
			m["node_uuid"] = *b.NodeUUID
		}
		dueSoon = append(dueSoon, m)
	}
	sort.Slice(dueSoon, func(i, j int) bool {
		return dueSoon[i]["next_due_date"].(*time.Time).Before(*dueSoon[j]["next_due_date"].(*time.Time))
	})

	var metered []fiber.Map
	items, err := h.DB.ExpenseItem.Query().Where(expenseitem.Active(true), expenseitem.PricingEQ(expenseitem.PricingMetered)).All(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		sum, err := h.Expenses.MeteredSummary(ctx, it, now)
		if err != nil {
			code, params := apperr.Fields(err)
			metered = append(metered, fiber.Map{"item_id": it.ID, "name": it.Name, "error": err.Error(), "error_code": code, "error_params": params})
			continue
		}
		h.Expenses.WithConsumers(ctx, sum, 5)
		metered = append(metered, meteredView(sum))
	}

	// Income/expenses by month for the last 12 months.
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, h.Config.Location).AddDate(0, -11, 0)
	points := make([]monthPoint, 12)
	idx := map[string]int{}
	for i := range points {
		m := from.AddDate(0, i, 0).Format("2006-01")
		points[i].Month = m
		idx[m] = i
	}
	pays, err := h.DB.Payment.Query().All(ctx)
	if err != nil {
		return err
	}
	var incomeTotal, incomeMonth int64
	thisMonth := now.Format("2006-01")
	for _, p := range pays {
		incomeTotal += p.Amount
		m := p.Date.In(h.Config.Location).Format("2006-01")
		if i, ok := idx[m]; ok {
			points[i].Income += money.ToMajor(p.Amount)
		}
		if m == thisMonth {
			incomeMonth += p.Amount
		}
	}
	exps, err := h.DB.Expense.Query().All(ctx)
	if err != nil {
		return err
	}
	var expTotal, expMonth int64
	for _, e := range exps {
		expTotal += e.RubAmount
		m := e.Date.In(h.Config.Location).Format("2006-01")
		if i, ok := idx[m]; ok {
			points[i].Expenses += money.ToMajor(e.RubAmount)
		}
		if m == thisMonth {
			expMonth += e.RubAmount
		}
	}

	accruals, err := h.DB.ReferralAccrual.Query().All(ctx)
	if err != nil {
		return err
	}
	var refTotal, refMonth int64
	for _, a := range accruals {
		refTotal += a.Amount
		if a.Date.In(h.Config.Location).Format("2006-01") == thisMonth {
			refMonth += a.Amount
		}
	}
	var debt int64
	for _, b := range balances {
		if b < 0 {
			debt += -b
		}
	}

	lastSync, syncErr, err := h.panelSync(c.UserContext())
	if err != nil {
		return err
	}
	syncInfo := fiber.Map{"last_sync": nil, "error": syncErr}
	if lastSync != nil {
		syncInfo["last_sync"] = lastSync
	}

	if expiring == nil {
		expiring = []ExpiringItem{}
	}
	return c.JSON(fiber.Map{
		"window_days":      window,
		"mrr":              mrr,
		"planned_expenses": money.ToMajor(plannedTotal),
		"profit":           mrr - money.ToMajor(plannedTotal),
		"active_customers": len(customers),
		"active_subs":      activeSubs,
		"active_addons":    activeAddons,
		"income_total":     money.ToMajor(incomeTotal),
		"expenses_total":   money.ToMajor(expTotal),
		"cash_balance":     money.ToMajor(incomeTotal - expTotal),
		"income_month":     money.ToMajor(incomeMonth),
		"expenses_month":   money.ToMajor(expMonth),
		"referral_total":   money.ToMajor(refTotal),
		"referral_month":   money.ToMajor(refMonth),
		"debt_total":       money.ToMajor(debt),
		"expiring":         expiring,
		"traffic_low":      trafficLow,
		"traffic_stats":    traffic,
		"due_soon":         dueSoon,
		"metered":          metered,
		"months":           points,
		"sync":             syncInfo,
	})
}
