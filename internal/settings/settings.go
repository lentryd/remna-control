// Package settings stores app settings editable from the UI as key/value
// rows, with typed accessors and defaults.
package settings

import (
	"context"
	"strconv"

	"remna-control/ent"
	"remna-control/ent/setting"
)

const (
	// ReferralPercent is the default % of a referee's payment accrued to
	// the referrer (customers can override it).
	ReferralPercent = "referral_percent"
	// ExpiringWindowDays is how far ahead the dashboard looks for expiries.
	ExpiringWindowDays = "expiring_window_days"
	// DefaultFeePercent pre-fills the bank/conversion fee of foreign
	// currency expenses.
	DefaultFeePercent = "default_fee_percent"
	// BaseCurrency is the ISO code every amount (payments, balances,
	// converted expenses) is kept in. It can't change once there's money
	// in the books.
	BaseCurrency = "base_currency"
	// SnapshotIntervalHours is how often an automatic backup snapshot is
	// taken (0 = never); SnapshotKeep is how many of them are kept.
	SnapshotIntervalHours = "snapshot_interval_hours"
	SnapshotKeep          = "snapshot_keep"
)

// Defaults are used when a key was never saved.
var Defaults = map[string]string{
	ReferralPercent:       "10",
	ExpiringWindowDays:    "7",
	DefaultFeePercent:     "0",
	BaseCurrency:          "RUB",
	SnapshotIntervalHours: "24",
	SnapshotKeep:          "14",
}

// Text lists the keys whose values aren't numbers.
var Text = map[string]bool{BaseCurrency: true}

type Store struct{ db *ent.Client }

func New(db *ent.Client) *Store { return &Store{db: db} }

// All returns every known setting, saved values over defaults.
func (s *Store) All(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(Defaults))
	for k, v := range Defaults {
		out[k] = v
	}
	rows, err := s.db.Setting.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}

func (s *Store) Get(ctx context.Context, key string) string {
	if r, err := s.db.Setting.Query().Where(setting.Key(key)).Only(ctx); err == nil {
		return r.Value
	}
	return Defaults[key]
}

func (s *Store) Float(ctx context.Context, key string) float64 {
	v, err := strconv.ParseFloat(s.Get(ctx, key), 64)
	if err != nil {
		v, _ = strconv.ParseFloat(Defaults[key], 64)
	}
	return v
}

// Base is the base currency.
func (s *Store) Base(ctx context.Context) string { return s.Get(ctx, BaseCurrency) }

func (s *Store) Int(ctx context.Context, key string) int {
	return int(s.Float(ctx, key))
}

func (s *Store) Set(ctx context.Context, key, value string) error {
	return s.db.Setting.Create().SetKey(key).SetValue(value).
		OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
}
