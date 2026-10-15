package handlers

import (
	"context"
	"time"

	"remna-control/ent"
	"remna-control/ent/predicate"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/internal/billing"
	"remna-control/internal/money"
	"remna-control/internal/remnawave"
)

type RwUserView struct {
	ID                   int        `json:"id"`
	Username             string     `json:"username"`
	ShortUUID            string     `json:"short_uuid"`
	Status               string     `json:"status"`
	ExpireAt             *time.Time `json:"expire_at"`
	Unlimited            bool       `json:"unlimited"`
	UsedTrafficBytes     int64      `json:"used_traffic_bytes"`
	TrafficLimitBytes    int64      `json:"traffic_limit_bytes"`
	TrafficLimitStrategy string     `json:"traffic_limit_strategy"`
	LastTrafficResetAt   *time.Time `json:"last_traffic_reset_at"`
	NextTrafficResetAt   *time.Time `json:"next_traffic_reset_at"`
	HwidDeviceLimit      *int       `json:"hwid_device_limit"`
	OnlineAt             *time.Time `json:"online_at"`
	Description          string     `json:"description"`
	Tag                  string     `json:"tag"`
	TelegramID           *int64     `json:"telegram_id"`
	SubscriptionURL      string     `json:"subscription_url"`
	SquadUUIDs           []string   `json:"squad_uuids"`
	Deleted              bool       `json:"deleted"`
	SyncedAt             time.Time  `json:"synced_at"`
}

func rwUserView(u *ent.RwUser) *RwUserView {
	if u == nil {
		return nil
	}
	return &RwUserView{
		ID: u.ID, Username: u.Username, ShortUUID: u.ShortUUID, Status: u.Status,
		ExpireAt: u.ExpireAt, Unlimited: billing.Unlimited(u.ExpireAt), UsedTrafficBytes: u.UsedTrafficBytes, TrafficLimitBytes: u.TrafficLimitBytes,
		TrafficLimitStrategy: u.TrafficLimitStrategy, LastTrafficResetAt: u.LastTrafficResetAt,
		HwidDeviceLimit: u.HwidDeviceLimit, OnlineAt: u.OnlineAt,
		Description: u.Description, Tag: u.Tag, TelegramID: u.TelegramID, SubscriptionURL: u.SubscriptionURL,
		SquadUUIDs: u.SquadUuids, Deleted: u.Deleted, SyncedAt: u.SyncedAt,
		NextTrafficResetAt: remnawave.NextTrafficReset(u.TrafficLimitStrategy, u.PanelCreatedAt, time.Now()),
	}
}

type AddonView struct {
	ID             int      `json:"id"`
	SubscriptionID int      `json:"subscription_id"`
	AddonID        int      `json:"addon_id"`
	AddonName      string   `json:"addon_name"`
	TariffID       *int     `json:"tariff_id"`
	TariffName     string   `json:"tariff_name"`
	PriceOverride  *float64 `json:"price_override"`
	Price          float64  `json:"price"`
	AutoExtend     bool     `json:"auto_extend"`
	// Included add-ons come with the subscription's tariff (price 0).
	Included bool        `json:"included"`
	RwUser   *RwUserView `json:"rw_user"`
}

type SubscriptionView struct {
	ID            int         `json:"id"`
	CustomerID    int         `json:"customer_id"`
	CustomerName  string      `json:"customer_name"`
	CustomerArch  bool        `json:"customer_archived"`
	TariffID      *int        `json:"tariff_id"`
	TariffName    string      `json:"tariff_name"`
	Label         string      `json:"label"`
	Title         string      `json:"title"`
	PriceOverride *float64    `json:"price_override"`
	Price         float64     `json:"price"`
	AutoExtend    bool        `json:"auto_extend"`
	RwUser        *RwUserView `json:"rw_user"`
	Addons        []AddonView `json:"addons"`
	CreatedAt     time.Time   `json:"created_at"`
}

// Monthly is the subscription's price plus its add-ons' prices (rubles).
func (s SubscriptionView) Monthly() float64 {
	m := s.Price
	for _, a := range s.Addons {
		m += a.Price
	}
	return m
}

func addonView(sa *ent.SubscriptionAddon) AddonView {
	v := AddonView{
		ID: sa.ID, SubscriptionID: sa.SubscriptionID, AddonID: sa.AddonID, TariffID: sa.TariffID,
		PriceOverride: money.ToMajorPtr(sa.PriceOverride),
		Price:         money.ToMajor(billing.AddonPrice(sa)),
		AutoExtend:    sa.AutoExtend, Included: sa.Included, RwUser: rwUserView(sa.Edges.RwUser),
	}
	if sa.Edges.Addon != nil {
		v.AddonName = sa.Edges.Addon.Name
	}
	if sa.Edges.Tariff != nil {
		v.TariffName = sa.Edges.Tariff.Name
	}
	return v
}

func subscriptionView(sub *ent.Subscription) SubscriptionView {
	v := SubscriptionView{
		ID: sub.ID, CustomerID: sub.CustomerID, TariffID: sub.TariffID, Label: sub.Label,
		Title:         billing.SubscriptionTitle(sub),
		PriceOverride: money.ToMajorPtr(sub.PriceOverride),
		Price:         money.ToMajor(billing.EffectivePrice(sub.PriceOverride, sub.Edges.Tariff)),
		AutoExtend:    sub.AutoExtend, RwUser: rwUserView(sub.Edges.RwUser),
		Addons: []AddonView{}, CreatedAt: sub.CreatedAt,
	}
	if c := sub.Edges.Customer; c != nil {
		v.CustomerName, v.CustomerArch = c.Name, c.Archived
	}
	if t := sub.Edges.Tariff; t != nil {
		v.TariffName = t.Name
	}
	for _, sa := range sub.Edges.Addons {
		v.Addons = append(v.Addons, addonView(sa))
	}
	return v
}

// loadSubscriptions loads subscriptions with everything the views need.
func (h *Handlers) loadSubscriptions(ctx context.Context, where ...func(*ent.SubscriptionQuery)) ([]SubscriptionView, error) {
	q := h.DB.Subscription.Query().
		WithCustomer().WithTariff().WithRwUser().
		WithAddons(func(q *ent.SubscriptionAddonQuery) { q.WithAddon().WithTariff().WithRwUser() }).
		Order(ent.Asc(subscription.FieldID))
	for _, w := range where {
		w(q)
	}
	subs, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SubscriptionView, 0, len(subs))
	for _, s := range subs {
		out = append(out, subscriptionView(s))
	}
	return out, nil
}

// live reports whether a subscription/add-on counts towards MRR: not yet
// linked (e.g. just imported) or linked to a panel user that wasn't deleted
// or disabled. Archived customers are excluded by the callers.
func live(u *RwUserView) bool {
	return u == nil || (!u.Deleted && u.Status != "DISABLED")
}

// paying is a live user that is billed: unlimited ones are free.
func paying(u *RwUserView) bool { return live(u) && (u == nil || !u.Unlimited) }

// monthlyLive is the live, billed part of a subscription's monthly price.
func monthlyLive(s SubscriptionView) float64 {
	var m float64
	if paying(s.RwUser) {
		m += s.Price
	}
	for _, a := range s.Addons {
		if a.RwUser != nil && paying(a.RwUser) {
			m += a.Price
		}
	}
	return m
}

func subscriptionAddonIDIn(ids []int) predicate.SubscriptionAddon {
	return subscriptionaddon.IDIn(ids...)
}
