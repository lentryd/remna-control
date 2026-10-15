// Package expenses books infrastructure costs in any currency (frozen in
// the base currency at the date's rate, with bank fee and cost-share),
// refunds, and
// traffic-metered items like a CDN billed per GB (see package metered).
package expenses

import (
	"math"
	"time"

	"remna-control/ent"
	"remna-control/internal/metered"
)

// ToRub converts minor units of a currency to kopecks:
// orig × rate × (1 + fee%) × share%.
func ToBase(orig int64, rate, feePercent, sharePercent float64) int64 {
	return int64(math.Round(float64(orig) * rate * (1 + feePercent/100) * sharePercent / 100))
}

// PricingOf is the metered pricing of an item.
func PricingOf(it *ent.ExpenseItem) metered.Pricing {
	return metered.Pricing{
		Unit: metered.Unit(it.GBUnit), MinMode: metered.MinMode(it.MinMode),
		PricePerGB: it.PricePerGB, MinCharge: it.MinCharge, FreeGB: it.FreeGB, Tiers: it.Tiers,
	}
}

// Forecast extrapolates a period's traffic: used so far plus the average of
// the last (up to) 7 complete days times the days left, today included.
// daily holds bytes per day of the period up to and including today.
func Forecast(daily []int64, today, periodEnd time.Time) int64 {
	var used int64
	for _, b := range daily {
		used += b
	}
	complete := daily
	if len(complete) > 0 {
		complete = complete[:len(complete)-1] // today is partial
	}
	if len(complete) > 7 {
		complete = complete[len(complete)-7:]
	}
	if len(complete) == 0 {
		return used
	}
	var sum int64
	for _, b := range complete {
		sum += b
	}
	avg := float64(sum) / float64(len(complete))

	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	daysLeft := int(periodEnd.Sub(todayDate).Hours()/24) + 1 // incl. today
	var todaySoFar int64
	if len(daily) > 0 {
		todaySoFar = daily[len(daily)-1]
	}
	rest := avg*float64(daysLeft) - float64(todaySoFar)
	if rest < 0 {
		rest = 0
	}
	return used + int64(rest)
}

// MonthBounds returns the first and last day of t's month.
func MonthBounds(t time.Time) (time.Time, time.Time) {
	return PeriodBounds(t, 1)
}

// PeriodBounds returns the first and last day of the billing period that
// contains t, for periods starting on billingDay of a month.
func PeriodBounds(t time.Time, billingDay int) (time.Time, time.Time) {
	billingDay = max(1, min(28, billingDay))
	start := time.Date(t.Year(), t.Month(), billingDay, 0, 0, 0, 0, t.Location())
	if t.Day() < billingDay {
		start = start.AddDate(0, -1, 0)
	}
	return start, start.AddDate(0, 1, -1)
}

// PeriodStart is the first day of the period keyed "YYYY-MM" (the month it
// starts in).
func PeriodStart(key string, billingDay int, loc *time.Location) (time.Time, error) {
	m, err := time.ParseInLocation("2006-01", key, loc)
	if err != nil {
		return time.Time{}, err
	}
	return m.AddDate(0, 0, max(1, min(28, billingDay))-1), nil
}
