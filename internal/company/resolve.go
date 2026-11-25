package company

import (
	"slices"

	"remna-control/ent"
)

// Field names that can be overridden for a member.
const (
	fieldPrice           = "price"
	fieldTrafficLimit    = "traffic_limit"
	fieldTrafficStrategy = "traffic_strategy"
	fieldHwidLimit       = "hwid_limit"
	fieldSquads          = "squads"
)

// Values are the parameters a member is billed and provisioned on.
type Values struct {
	Price             int64 // monthly, kopecks
	TrafficLimitBytes int64
	TrafficStrategy   string
	HwidLimit         *int
	Squads            []string
}

// Overrides sets fields for one member; nil/empty fields are left alone.
// Reset names fields (the constants above) that go back to the tariff.
type Overrides struct {
	Price             *int64
	TrafficLimitBytes *int64
	TrafficStrategy   *string
	HwidLimit         *int
	Squads            []string
	Reset             []string
}

// ValuesOf is what tariff t gives.
func ValuesOf(t *ent.Tariff) Values {
	return Values{
		Price:             t.MonthlyPrice,
		TrafficLimitBytes: t.TrafficLimitBytes,
		TrafficStrategy:   t.TrafficStrategy,
		HwidLimit:         t.HwidLimit,
		Squads:            slices.Clone(t.SquadUuids),
	}
}

// Resolve takes every field not listed in overrides from tariff t and keeps
// the rest from current.
func Resolve(t *ent.Tariff, current Values, overrides []string) Values {
	v := ValuesOf(t)
	has := func(f string) bool { return slices.Contains(overrides, f) }
	if has(fieldPrice) {
		v.Price = current.Price
	}
	if has(fieldTrafficLimit) {
		v.TrafficLimitBytes = current.TrafficLimitBytes
	}
	if has(fieldTrafficStrategy) {
		v.TrafficStrategy = current.TrafficStrategy
	}
	if has(fieldHwidLimit) {
		v.HwidLimit = current.HwidLimit
	}
	if has(fieldSquads) {
		v.Squads = slices.Clone(current.Squads)
	}
	return v
}

// apply returns the values and override list after o, on tariff t, starting
// from the segment's current state.
func apply(t *ent.Tariff, current Values, overrides []string, o Overrides) (Values, []string) {
	keep := slices.DeleteFunc(slices.Clone(overrides), func(f string) bool { return slices.Contains(o.Reset, f) })
	v := Resolve(t, current, keep)
	add := func(f string) {
		if !slices.Contains(keep, f) {
			keep = append(keep, f)
		}
	}
	if o.Price != nil {
		v.Price = *o.Price
		add(fieldPrice)
	}
	if o.TrafficLimitBytes != nil {
		v.TrafficLimitBytes = *o.TrafficLimitBytes
		add(fieldTrafficLimit)
	}
	if o.TrafficStrategy != nil {
		v.TrafficStrategy = *o.TrafficStrategy
		add(fieldTrafficStrategy)
	}
	if o.HwidLimit != nil {
		h := *o.HwidLimit
		v.HwidLimit = &h
		add(fieldHwidLimit)
	}
	if o.Squads != nil {
		v.Squads = slices.Clone(o.Squads)
		add(fieldSquads)
	}
	slices.Sort(keep)
	return v, keep
}

func sameValues(a, b Values) bool {
	hwid := func(p *int) int {
		if p == nil {
			return -1
		}
		return *p
	}
	return a.Price == b.Price && a.TrafficLimitBytes == b.TrafficLimitBytes && a.TrafficStrategy == b.TrafficStrategy &&
		hwid(a.HwidLimit) == hwid(b.HwidLimit) && sameSet(a.Squads, b.Squads)
}

func segmentValues(s *ent.MemberSegment) Values {
	return Values{Price: s.Price, TrafficLimitBytes: s.TrafficLimitBytes, TrafficStrategy: s.TrafficStrategy, HwidLimit: s.HwidLimit, Squads: s.SquadUuids}
}
