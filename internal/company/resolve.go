package company

import (
	"slices"
	"strings"

	"remna-control/ent"
)

// Field names that can be overridden for a member.
const (
	fieldPrice           = "price"
	fieldTrafficLimit    = "traffic_limit"
	fieldTrafficStrategy = "traffic_strategy"
	fieldHwidLimit       = "hwid_limit"
	fieldSquads          = "squads"
	fieldExternalSquad   = "external_squad"
)

// Values are the parameters a member is billed and provisioned on.
type Values struct {
	Price             int64 // monthly, kopecks
	TrafficLimitBytes int64
	TrafficStrategy   string
	HwidLimit         *int
	Squads            []string
	// ExternalSquad: nil = nothing set anywhere (the panel is left alone),
	// "" = explicitly none, else a uuid.
	ExternalSquad *string
}

// Overrides sets fields for one member; nil/empty fields are left alone.
// Reset names fields (the constants above) that go back to the tariff.
type Overrides struct {
	Price             *int64
	TrafficLimitBytes *int64
	TrafficStrategy   *string
	HwidLimit         *int
	Squads            []string
	// ExternalSquad: "" means explicitly none for this employee.
	ExternalSquad *string
	Reset         []string
}

// ValuesOf is what tariff t gives under company c: c's external squad, when
// it has one, beats the tariff's.
func ValuesOf(t *ent.Tariff, c *ent.Company) Values {
	ext := t.ExternalSquadUUID
	if c != nil && c.ExternalSquadUUID != nil {
		ext = c.ExternalSquadUUID
	}
	return Values{
		ExternalSquad:     cloneStr(ext),
		Price:             t.MonthlyPrice,
		TrafficLimitBytes: t.TrafficLimitBytes,
		TrafficStrategy:   t.TrafficStrategy,
		HwidLimit:         t.HwidLimit,
		Squads:            slices.Clone(t.SquadUuids),
	}
}

// Resolve takes every field not listed in overrides from tariff t and keeps
// the rest from current.
func Resolve(t *ent.Tariff, c *ent.Company, current Values, overrides []string) Values {
	v := ValuesOf(t, c)
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
	if has(fieldExternalSquad) {
		v.ExternalSquad = cloneStr(current.ExternalSquad)
	}
	return v
}

// apply returns the values and override list after o, on tariff t, starting
// from the segment's current state.
func apply(t *ent.Tariff, c *ent.Company, current Values, overrides []string, o Overrides) (Values, []string) {
	keep := slices.DeleteFunc(slices.Clone(overrides), func(f string) bool { return slices.Contains(o.Reset, f) })
	v := Resolve(t, c, current, keep)
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
	if o.ExternalSquad != nil {
		v.ExternalSquad = cloneStr(o.ExternalSquad)
		add(fieldExternalSquad)
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
		hwid(a.HwidLimit) == hwid(b.HwidLimit) && sameSet(a.Squads, b.Squads) && sameStr(a.ExternalSquad, b.ExternalSquad)
}

func segmentValues(s *ent.MemberSegment) Values {
	return Values{Price: s.Price, TrafficLimitBytes: s.TrafficLimitBytes, TrafficStrategy: s.TrafficStrategy, HwidLimit: s.HwidLimit, Squads: s.SquadUuids, ExternalSquad: cloneStr(s.ExternalSquadUUID)}
}

func cloneStr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func sameStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// pushValues is v as it should reach the panel after the change from old: a
// squad that was set and now resolves to nothing is cleared explicitly,
// since nil means "leave the panel alone".
func pushValues(old, v Values) Values {
	if v.ExternalSquad == nil && old.ExternalSquad != nil && *old.ExternalSquad != "" {
		v.ExternalSquad = cloneStr(new(string))
	}
	return v
}

// blankToNil turns a missing or blank string into nil and trims the rest.
func blankToNil(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}
