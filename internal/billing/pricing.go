// Package billing holds customer balances, payments, referral accruals and
// paid extensions of subscriptions/add-ons in Remnawave.
package billing

import (
	"math"
	"sort"
	"time"
)

// Period is a fixed price for a term (kopecks): a whole-month discount
// (Days == 0) or a package with days, e.g. a week-long trial.
type Period struct {
	Months int   `json:"months"`
	Days   int   `json:"days"`
	Price  int64 `json:"price"`
}

// maxMonths caps how far a single payment can extend one item.
const maxMonths = 36

// Cost is the cheapest price for months, combining whole-month discounted
// periods and the plain monthly price.
func Cost(monthly int64, periods []Period, months int) int64 {
	if months <= 0 {
		return 0
	}
	best := make([]int64, months+1)
	for m := 1; m <= months; m++ {
		best[m] = best[m-1] + monthly
		for _, p := range periods {
			if p.Days == 0 && p.Months > 0 && p.Months <= m && best[m-p.Months]+p.Price < best[m] {
				best[m] = best[m-p.Months] + p.Price
			}
		}
	}
	return best[months]
}

// DaysCost is the price of extra days at monthly/30 per day.
func DaysCost(monthly int64, days int) int64 {
	return int64(math.Round(float64(monthly) * float64(days) / 30))
}

// TermCost prices months+days: a package with exactly that term (one with
// days) costs its own price, anything else is Cost plus DaysCost.
func TermCost(monthly int64, periods []Period, months, days int) int64 {
	if days > 0 {
		for _, p := range periods {
			if p.Months == months && p.Days == days {
				return p.Price
			}
		}
	}
	return Cost(monthly, periods, months) + DaysCost(monthly, days)
}

// UnlimitedYear: the panel's "forever" is an expiry in 2099; anything from
// this year on counts as an unlimited (never expiring, unpaid) user.
const UnlimitedYear = 2090

// Unlimited reports whether expireAt means "forever".
func Unlimited(expireAt *time.Time) bool {
	return expireAt != nil && expireAt.Year() >= UnlimitedYear
}

// ExtendFrom is where an extension starts: the current expiry if it's still
// in the future, otherwise now.
func ExtendFrom(now time.Time, expireAt *time.Time) time.Time {
	if expireAt != nil && expireAt.After(now) {
		return *expireAt
	}
	return now
}

// ProrateSurcharge is what switching from oldMonthly to newMonthly costs
// for the time left until expireAt (negative for a downgrade).
func ProrateSurcharge(oldMonthly, newMonthly int64, now time.Time, expireAt *time.Time) int64 {
	if expireAt == nil || !expireAt.After(now) || Unlimited(expireAt) {
		return 0
	}
	days := expireAt.Sub(now).Hours() / 24
	return int64(math.Round(float64(newMonthly-oldMonthly) * days / 30))
}

// maxExchangeDays caps the term a tariff exchange can give (2 years).
const maxExchangeDays = 730

// ExchangeDays is how many days a tariff switch gives when the remaining
// time on the old tariff is converted into the new one instead of paying a
// surcharge: credit = oldMonthly * daysLeft / 30, days = credit / newMonthly * 30.
// 0 means the exchange isn't offered (expired, unlimited, free target or
// under a day); the result is capped at 730 days.
func ExchangeDays(oldMonthly, newMonthly int64, now time.Time, expireAt *time.Time) int {
	if expireAt == nil || !expireAt.After(now) || Unlimited(expireAt) || newMonthly <= 0 || oldMonthly <= 0 {
		return 0
	}
	daysLeft := expireAt.Sub(now).Hours() / 24
	credit := float64(oldMonthly) * daysLeft / 30
	days := math.Round(credit / float64(newMonthly) * 30)
	if days < 1 {
		return 0
	}
	if days > maxExchangeDays {
		return maxExchangeDays
	}
	return int(days)
}

// PlanItem is something a payment can extend.
type PlanItem struct {
	Kind     string     `json:"kind"` // "subscription" | "addon"
	ID       int        `json:"id"`
	Title    string     `json:"title"`
	Monthly  int64      `json:"monthly"`
	Periods  []Period   `json:"periods,omitempty"`
	ExpireAt *time.Time `json:"expire_at"`
	// ParentID/ParentExpireAt (add-ons only) tie an add-on to its
	// subscription: it's never extended past it (see addonSlack).
	ParentID       int        `json:"parent_id,omitempty"`
	ParentExpireAt *time.Time `json:"-"`
	// AutoExtend items get a share of a payment by default.
	AutoExtend bool `json:"auto_extend"`
}

// Allocation is how much of a payment goes to one item.
type Allocation struct {
	Kind   string    `json:"kind"`
	ID     int       `json:"id"`
	Title  string    `json:"title"`
	Months int       `json:"months"`
	Days   int       `json:"days"`
	Amount int64     `json:"amount"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

// addonSlack is how far an add-on may outlast its subscription: add-ons are
// often connected mid-period, so their cycles are a bit out of step.
const addonSlack = 15 * 24 * time.Hour

// Plan spreads balance over items one month at a time, always to the item
// whose (planned) expiry is earliest, so paid-up dates stay in step; a
// month is given while the balance covers its marginal cost (which already
// accounts for period discounts). Add-ons never go past their subscription.
// With remainderToDays the leftover becomes extra days on the item that
// ends first.
func Plan(balance int64, items []PlanItem, now time.Time, remainderToDays bool) ([]Allocation, int64) {
	type state struct {
		PlanItem
		from   time.Time
		months int
		days   int
	}
	var st []*state
	subIdx := map[int]*state{}
	for _, it := range items {
		if it.Monthly <= 0 {
			continue
		}
		s := &state{PlanItem: it, from: ExtendFrom(now, it.ExpireAt)}
		st = append(st, s)
		if it.Kind != "addon" {
			subIdx[it.ID] = s
		}
	}
	to := func(s *state) time.Time { return s.from.AddDate(0, s.months, s.days) }
	capOf := func(s *state) (time.Time, bool) {
		if s.Kind != "addon" || s.ParentID == 0 {
			return time.Time{}, false
		}
		if p, ok := subIdx[s.ParentID]; ok {
			return to(p).Add(addonSlack), true
		}
		if s.ParentExpireAt != nil {
			return ExtendFrom(now, s.ParentExpireAt).Add(addonSlack), true
		}
		return time.Time{}, false
	}

	remaining := balance
	for {
		var best *state
		var bestDelta int64
		for _, s := range st {
			if s.months >= maxMonths {
				continue
			}
			delta := Cost(s.Monthly, s.Periods, s.months+1) - Cost(s.Monthly, s.Periods, s.months)
			if delta > remaining {
				continue
			}
			if c, ok := capOf(s); ok && s.from.AddDate(0, s.months+1, 0).After(c) {
				continue
			}
			if best == nil || to(s).Before(to(best)) || (to(s).Equal(to(best)) && best.Kind == "addon" && s.Kind != "addon") {
				best, bestDelta = s, delta
			}
		}
		if best == nil {
			break
		}
		best.months++
		remaining -= bestDelta
	}

	if remainderToDays && remaining > 0 && len(st) > 0 {
		first := st[0]
		for _, s := range st[1:] {
			if to(s).Before(to(first)) && s.Kind != "addon" {
				first = s
			}
		}
		if d := int(remaining * 30 / first.Monthly); d > 0 {
			first.days = d
		}
	}

	var out []Allocation
	for _, s := range st {
		if s.months == 0 && s.days == 0 {
			continue
		}
		out = append(out, Allocation{
			Kind: s.Kind, ID: s.ID, Title: s.Title, Months: s.months, Days: s.days,
			Amount: TermCost(s.Monthly, s.Periods, s.months, s.days),
			From:   s.from, To: to(s),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].From.Before(out[j].From) })
	rest := balance - sumAmounts(out)
	if rest < 0 { // rounding of days
		rest = 0
	}
	return out, rest
}

func sumAmounts(a []Allocation) int64 {
	var s int64
	for _, x := range a {
		s += x.Amount
	}
	return s
}

// ReferralAmount is percent of amount, rounded to kopecks.
func ReferralAmount(amount int64, percent float64) int64 {
	return int64(math.Round(float64(amount) * percent / 100))
}
