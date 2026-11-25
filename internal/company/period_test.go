package company

import (
	"testing"
	"time"
)

func day(loc *time.Location, y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func TestPeriodContaining(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		name      string
		reportDay int
		at        time.Time
		from, to  time.Time
		days      int
	}{
		{"after report day", 15, time.Date(2026, 11, 20, 13, 0, 0, 0, loc), day(loc, 2026, 11, 15), day(loc, 2026, 12, 15), 30},
		{"before report day", 15, day(loc, 2026, 11, 10), day(loc, 2026, 10, 15), day(loc, 2026, 11, 15), 31},
		{"on report day starts a new period", 15, day(loc, 2026, 11, 15), day(loc, 2026, 11, 15), day(loc, 2026, 12, 15), 30},
		{"day 1", 1, day(loc, 2026, 11, 30), day(loc, 2026, 11, 1), day(loc, 2026, 12, 1), 30},
		{"across the year", 1, day(loc, 2026, 12, 31), day(loc, 2026, 12, 1), day(loc, 2027, 1, 1), 31},
		{"february ends on 28", 28, day(loc, 2027, 2, 10), day(loc, 2027, 1, 28), day(loc, 2027, 2, 28), 31},
		{"february starts on 28", 28, day(loc, 2027, 3, 1), day(loc, 2027, 2, 28), day(loc, 2027, 3, 28), 28},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := PeriodContaining(c.reportDay, c.at, loc)
			if !p.From.Equal(c.from) || !p.To.Equal(c.to) || p.Days() != c.days {
				t.Errorf("got [%s, %s) %d days, want [%s, %s) %d days", p.From, p.To, p.Days(), c.from, c.to, c.days)
			}
		})
	}
}

func TestNextReportDate(t *testing.T) {
	loc := time.UTC
	if got := NextReportDate(day(loc, 2026, 11, 15), 15, loc); !got.Equal(day(loc, 2026, 12, 15)) {
		t.Errorf("strictly after the report date itself: got %s", got)
	}
	if got := NextReportDate(day(loc, 2026, 11, 10), 15, loc); !got.Equal(day(loc, 2026, 11, 15)) {
		t.Errorf("same month: got %s", got)
	}
	if got := NextReportDate(day(loc, 2026, 12, 20), 5, loc); !got.Equal(day(loc, 2027, 1, 5)) {
		t.Errorf("next year: got %s", got)
	}
}

func TestDaysIn(t *testing.T) {
	loc := time.UTC
	p := Period{From: day(loc, 2026, 11, 1), To: day(loc, 2026, 12, 1)} // 30 days
	ptr := func(t time.Time) *time.Time { return &t }
	cases := []struct {
		name string
		from time.Time
		to   *time.Time
		want int
	}{
		{"whole period, open", day(loc, 2026, 10, 1), nil, 30},
		{"added on day 19 counts that day", time.Date(2026, 11, 19, 15, 30, 0, 0, loc), nil, 12},
		{"removed after 24 days, remove day not counted", day(loc, 2026, 10, 1), ptr(day(loc, 2026, 11, 25)), 24},
		{"added and removed on the same day", day(loc, 2026, 11, 10), ptr(day(loc, 2026, 11, 10)), 0},
		{"removed before the period", day(loc, 2026, 9, 1), ptr(day(loc, 2026, 10, 20)), 0},
		{"added after the period", day(loc, 2026, 12, 1), nil, 0},
		{"closed after the period end is clipped", day(loc, 2026, 10, 1), ptr(day(loc, 2027, 1, 1)), 30},
		{"inverted range", day(loc, 2026, 11, 20), ptr(day(loc, 2026, 11, 10)), 0},
	}
	for _, c := range cases {
		if got := DaysIn(p, c.from, c.to, loc); got != c.want {
			t.Errorf("%s: DaysIn = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestPeriodDaysAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	p := PeriodContaining(1, time.Date(2026, 3, 15, 12, 0, 0, 0, loc), loc)
	if p.Days() != 31 {
		t.Errorf("March 2026 in New York has %d days, want 31", p.Days())
	}
	if got := DaysIn(p, day(loc, 2026, 3, 1), nil, loc); got != 31 {
		t.Errorf("DaysIn across DST = %d, want 31", got)
	}
}

func TestProrate(t *testing.T) {
	cases := []struct {
		monthly      int64
		days, period int
		want         int64
	}{
		{50000, 12, 30, 20000},
		{50000, 1, 31, 1613},
		{50000, 0, 30, 0},
		{50000, 30, 30, 50000},
		{50000, 24, 30, 40000},
	}
	for _, c := range cases {
		if got := Prorate(c.monthly, c.days, c.period); got != c.want {
			t.Errorf("Prorate(%d, %d, %d) = %d, want %d", c.monthly, c.days, c.period, got, c.want)
		}
	}
}

func TestPeriodDaysEastOfUTC(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Skip("no tzdata")
	}
	p := PeriodContaining(1, day(loc, 2026, 11, 10), loc)
	if p.Days() != 30 {
		t.Errorf("November in Moscow has %d days, want 30", p.Days())
	}
}
