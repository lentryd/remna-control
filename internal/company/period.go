// Package company bills companies: employees live on date segments and are
// charged to the company's balance by a statement on each report date.
package company

import "time"

// Period is the half-open range [From, To) of calendar days; both ends are
// midnights in the location the period was built with.
type Period struct{ From, To time.Time }

// calendarDay is the midnight of t's date in loc.
func calendarDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// dayNumber counts calendar days, so a DST change never makes a day 23 or 25 hours.
func dayNumber(t time.Time, loc *time.Location) int {
	y, m, d := t.In(loc).Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// reportDate is reportDay of the month containing the (year, month) offset.
func reportDate(year int, month time.Month, reportDay int, loc *time.Location) time.Time {
	return time.Date(year, month, reportDay, 0, 0, 0, 0, loc)
}

// PeriodContaining returns the cycle, from one reportDay to the next, that
// contains the calendar day of t.
func PeriodContaining(reportDay int, t time.Time, loc *time.Location) Period {
	y, m, d := t.In(loc).Date()
	start := reportDate(y, m, reportDay, loc)
	if d < reportDay {
		start = reportDate(y, m-1, reportDay, loc)
	}
	sy, sm, _ := start.Date()
	return Period{From: start, To: reportDate(sy, sm+1, reportDay, loc)}
}

// NextReportDate returns the first report date strictly after the calendar
// day of after.
func NextReportDate(after time.Time, reportDay int, loc *time.Location) time.Time {
	return PeriodContaining(reportDay, after, loc).To
}

// Days is the number of calendar days in the period.
func (p Period) Days() int {
	return dayNumber(p.To, p.To.Location()) - dayNumber(p.From, p.From.Location())
}

// DaysIn counts the days of [from, to) that fall inside p, from the add day
// up to, but not including, the removal day. A nil to is an open end.
func DaysIn(p Period, from time.Time, to *time.Time, loc *time.Location) int {
	start := dayNumber(from, loc)
	end := dayNumber(p.To, loc)
	if to != nil {
		end = min(end, dayNumber(*to, loc))
	}
	start = max(start, dayNumber(p.From, loc))
	return max(end-start, 0)
}

// Prorate is the share of a monthly price for days out of periodDays,
// rounded half up to the kopeck.
func Prorate(monthly int64, days, periodDays int) int64 {
	if days <= 0 || periodDays <= 0 {
		return 0
	}
	n := monthly * int64(days)
	return (2*n + int64(periodDays)) / (2 * int64(periodDays))
}
