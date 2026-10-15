// Package fx converts currencies to the app's base currency and caches the
// rates per date in the database. Rates come from the Central Bank of Russia
// for a RUB base and from the European Central Bank for any other base.
package fx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"remna-control/ent"
	"remna-control/ent/fxrate"
)

// Source publishes rates for one date: units of base per one unit of each
// currency. Weekends and holidays answer with the last published rates.
type Source interface {
	Name() string
	Fetch(ctx context.Context, date time.Time) (map[string]float64, error)
}

// SourceFor picks the rate source for a base currency.
func SourceFor(base string, hc *http.Client) Source {
	if base == "RUB" {
		return &CBR{http: hc}
	}
	return &ECB{http: hc, base: base}
}

type Service struct {
	db   *ent.Client
	http *http.Client
	base func(context.Context) string
	mu   sync.Mutex
}

// New returns a Service converting to the currency base returns.
func New(db *ent.Client, base func(context.Context) string) *Service {
	return &Service{db: db, http: &http.Client{Timeout: 15 * time.Second}, base: base}
}

// Base is the current base currency.
func (s *Service) Base(ctx context.Context) string {
	return strings.ToUpper(s.base(ctx))
}

// Rate returns units of the base currency per one unit of currency on date.
// The base currency itself is always 1.
func (s *Service) Rate(ctx context.Context, currency string, date time.Time) (float64, error) {
	base := s.Base(ctx)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" || currency == base {
		return 1, nil
	}
	day := date.Format("2006-01-02")
	// Rates for a future date don't exist yet; use today's.
	if today := time.Now().Format("2006-01-02"); day > today {
		day = today
	}

	if r, err := s.db.FxRate.Query().Where(fxrate.Date(day), fxrate.Currency(currency), fxrate.Base(base)).Only(ctx); err == nil {
		return r.Rate, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	d, _ := time.Parse("2006-01-02", day)
	src := SourceFor(base, s.http)
	rates, err := src.Fetch(ctx, d)
	if err != nil {
		return 0, err
	}
	// Don't cache today's rate permanently: it may still change before the
	// day's publication settles.
	if day < time.Now().Format("2006-01-02") {
		for cur, rate := range rates {
			_ = s.db.FxRate.Create().SetDate(day).SetCurrency(cur).SetBase(base).SetRate(rate).
				OnConflictColumns(fxrate.FieldDate, fxrate.FieldCurrency, fxrate.FieldBase).UpdateNewValues().Exec(ctx)
		}
	}
	rate, ok := rates[currency]
	if !ok {
		return 0, fmt.Errorf("%s has no %s rate for %s", src.Name(), base, currency)
	}
	return rate, nil
}
