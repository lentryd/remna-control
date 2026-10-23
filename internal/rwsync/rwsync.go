// Package rwsync keeps the local Remnawave user cache (RwUser) and the
// add-on catalog in step with the panel and addons.yml.
package rwsync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"remna-control/ent"
	"remna-control/ent/addon"
	"remna-control/ent/customer"
	"remna-control/ent/rwuser"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/ent/tariff"
	"remna-control/internal/addons"
	"remna-control/internal/jobs"
	"remna-control/internal/remnawave"
)

type Service struct {
	db         *ent.Client
	rw         *remnawave.Client
	addonsPath string
}

func New(db *ent.Client, rw *remnawave.Client, addonsPath string) *Service {
	return &Service{db: db, rw: rw, addonsPath: addonsPath}
}

// stats is what a full sync did: users seen in the panel, and how many of
// them are new to the cache (or came back) or were marked deleted.
type stats struct {
	Users, Added, Removed int
}

// SyncJob refreshes the add-on catalog and every user; it reports a change
// when users were added or removed.
func (s *Service) SyncJob(ctx context.Context) (jobs.Result, error) {
	st, err := s.syncAll(ctx)
	if err != nil {
		return jobs.Result{}, err
	}
	return jobs.Result{
		Summary: fmt.Sprintf("synced %d users", st.Users),
		Changed: st.Added > 0 || st.Removed > 0,
	}, nil
}

func (s *Service) syncAll(ctx context.Context) (stats, error) {
	var st stats
	if err := s.SyncAddons(ctx); err != nil {
		return st, fmt.Errorf("sync addons: %w", err)
	}
	users, err := s.rw.AllUsers(ctx)
	if err != nil {
		return st, err
	}
	alive, err := s.db.RwUser.Query().Where(rwuser.Deleted(false)).IDs(ctx)
	if err != nil {
		return st, err
	}
	known := make(map[int]bool, len(alive))
	for _, id := range alive {
		known[id] = true
	}
	seen := make([]int, 0, len(users))
	for i := range users {
		if err := Upsert(ctx, s.db, &users[i]); err != nil {
			return st, err
		}
		if !known[users[i].ID] {
			st.Added++
		}
		seen = append(seen, users[i].ID)
	}
	st.Users = len(users)
	if st.Removed, err = s.db.RwUser.Update().
		Where(rwuser.IDNotIn(seen...), rwuser.Deleted(false)).
		SetDeleted(true).Save(ctx); err != nil {
		return st, fmt.Errorf("mark deleted users: %w", err)
	}
	if err := s.linkAddonUsers(ctx); err != nil {
		return st, fmt.Errorf("link addon users: %w", err)
	}
	if err := s.pullCustomerTelegram(ctx); err != nil {
		return st, fmt.Errorf("pull customer telegram: %w", err)
	}
	slog.Debug("remnawave sync done", "users", st.Users, "added", st.Added, "removed", st.Removed)
	return st, nil
}

// SyncAddons mirrors the add-ons file into the Addon table (matched by
// name); add-ons made in the UI are left alone.
func (s *Service) SyncAddons(ctx context.Context) error {
	list, err := addons.Load(s.addonsPath)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(list))
	for _, a := range list {
		name := a.DisplayName()
		names = append(names, name)
		if err := s.db.Addon.Create().
			SetName(name).SetPrefix(a.Prefix).SetSuffix(a.Suffix).SetInConfig(true).SetSource(addon.SourceFile).
			SetRemark(a.Remark).SetRemarkUnlimited(a.RemarkUnlimited).SetStubs(a.Stubs).
			OnConflictColumns(addon.FieldName).
			UpdatePrefix().UpdateSuffix().UpdateInConfig().UpdateSource().
			UpdateRemark().UpdateRemarkUnlimited().UpdateStubs().UpdateUpdatedAt().
			Exec(ctx); err != nil {
			return err
		}
	}
	_, err = s.db.Addon.Update().Where(addon.SourceEQ(addon.SourceFile), addon.NameNotIn(names...)).SetInConfig(false).Save(ctx)
	return err
}

// linkAddonUsers attaches panel users named like an add-on of a linked
// subscription (prefix+<main username>+suffix) to that subscription, so
// add-ons created by hand in the panel show up too.
func (s *Service) linkAddonUsers(ctx context.Context) error {
	addonList, err := s.db.Addon.Query().All(ctx)
	if err != nil || len(addonList) == 0 {
		return err
	}
	users, err := s.db.RwUser.Query().
		Where(rwuser.Deleted(false)).
		WithSubscription().WithSubscriptionAddon().
		All(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]*ent.RwUser, len(users))
	for _, u := range users {
		byName[u.Username] = u
	}
	for _, u := range users {
		if u.Edges.Subscription != nil || u.Edges.SubscriptionAddon != nil {
			continue
		}
		for _, a := range addonList {
			main, ok := MainUsername(u.Username, a.Prefix, a.Suffix)
			if !ok {
				continue
			}
			mainUser := byName[main]
			if mainUser == nil || mainUser.Edges.Subscription == nil {
				continue
			}
			sub := mainUser.Edges.Subscription
			exists, err := s.db.SubscriptionAddon.Query().
				Where(subscriptionaddon.SubscriptionID(sub.ID), subscriptionaddon.AddonID(a.ID)).
				Exist(ctx)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			q := s.db.SubscriptionAddon.Create().
				SetSubscriptionID(sub.ID).SetAddonID(a.ID).SetRwUserID(u.ID)
			// The subscription's tariff may include this add-on.
			if sub.TariffID != nil {
				inc, err := s.db.Tariff.Query().
					Where(tariff.AddonID(a.ID), tariff.HasIncludedInWith(tariff.ID(*sub.TariffID))).
					First(ctx)
				if err != nil && !ent.IsNotFound(err) {
					return err
				}
				if inc != nil {
					q.SetIncluded(true).SetTariffID(inc.ID)
				}
			}
			if err := q.Exec(ctx); err != nil {
				return err
			}
			slog.Info("linked add-on user", "username", u.Username, "subscription", sub.ID, "addon", a.Name)
			break
		}
	}
	return nil
}

// MainUsername strips an add-on's prefix/suffix; ok is false if username
// isn't shaped like that add-on's user.
func MainUsername(username, prefix, suffix string) (string, bool) {
	if prefix == "" && suffix == "" {
		return "", false
	}
	if !strings.HasPrefix(username, prefix) || !strings.HasSuffix(username, suffix) {
		return "", false
	}
	main := strings.TrimSuffix(strings.TrimPrefix(username, prefix), suffix)
	return main, main != "" && len(main) < len(username)
}

// Upsert stores a panel user in the cache.
func Upsert(ctx context.Context, db *ent.Client, u *remnawave.User) error {
	q := db.RwUser.Create().
		SetID(u.ID).
		SetUsername(u.Username).
		SetShortUUID(u.ShortUUID).
		SetStatus(u.Status).
		SetExpireAt(u.ExpireAt).
		SetUsedTrafficBytes(u.UserTraffic.UsedTrafficBytes).
		SetTrafficLimitBytes(u.TrafficLimitBytes).
		SetTrafficLimitStrategy(u.TrafficLimitStrategy).
		SetNillableLastTrafficResetAt(u.LastTrafficResetAt).
		SetNillableHwidDeviceLimit(u.HwidDeviceLimit).
		SetNillableOnlineAt(u.UserTraffic.OnlineAt).
		SetDescription(deref(u.Description)).
		SetTag(deref(u.Tag)).
		SetNillableTelegramID(u.TelegramID).
		SetSubscriptionURL(u.SubscriptionURL).
		SetSquadUuids(u.SquadUUIDs()).
		SetDeleted(false).
		SetSyncedAt(time.Now())
	err := q.OnConflictColumns(rwuser.FieldID).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("upsert rw user %d: %w", u.ID, err)
	}
	// UpdateNewValues doesn't null out fields that became nil.
	upd := db.RwUser.UpdateOneID(u.ID)
	if u.HwidDeviceLimit == nil {
		upd.ClearHwidDeviceLimit()
	}
	if u.UserTraffic.OnlineAt == nil {
		upd.ClearOnlineAt()
	}
	if u.TelegramID == nil {
		upd.ClearTelegramID()
	}
	if u.LastTrafficResetAt == nil {
		upd.ClearLastTrafficResetAt()
	}
	if !u.CreatedAt.IsZero() {
		upd.SetPanelCreatedAt(u.CreatedAt)
	}
	return upd.Exec(ctx)
}

// MarkDeleted flags a cached user as gone from the panel.
func MarkDeleted(ctx context.Context, db *ent.Client, id int) error {
	_, err := db.RwUser.Update().Where(rwuser.ID(id)).SetDeleted(true).Save(ctx)
	return err
}

// VerifySignature checks X-Remnawave-Signature: hex HMAC-SHA256 of the raw
// body keyed with the panel's WEBHOOK_SECRET_HEADER.
func VerifySignature(secret string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(strings.TrimSpace(signature))
	return err == nil && hmac.Equal(want, got)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// customerRwUsers returns the cached panel users of a customer: the main
// subscription users and their add-on users.
func customerRwUsers(ctx context.Context, db *ent.Client, customerID int) ([]*ent.RwUser, error) {
	return db.RwUser.Query().
		Where(rwuser.Deleted(false), rwuser.Or(
			rwuser.HasSubscriptionWith(subscription.CustomerID(customerID)),
			rwuser.HasSubscriptionAddonWith(subscriptionaddon.HasSubscriptionWith(subscription.CustomerID(customerID))),
		)).All(ctx)
}

// PushCustomerTelegram writes the customer's telegram id to every one of
// their panel users that differs; nil clears it. Failures on single users
// don't stop the rest, and are returned joined.
func (s *Service) PushCustomerTelegram(ctx context.Context, customerID int, tgID *int64) error {
	users, err := customerRwUsers(ctx, s.db, customerID)
	if err != nil {
		return err
	}
	var errs []error
	for _, u := range users {
		if sameInt64(u.TelegramID, tgID) {
			continue
		}
		upd := remnawave.UpdateUserRequest{ID: u.ID, TelegramID: tgID, ClearTelegramID: tgID == nil}
		got, err := s.rw.UpdateUser(ctx, upd)
		if err != nil {
			errs = append(errs, fmt.Errorf("user %s: %w", u.Username, err))
			continue
		}
		if err := Upsert(ctx, s.db, got); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// pullCustomerTelegram fills in the telegram id of customers that have none
// from their panel users (the panel is where it is often set first).
func (s *Service) pullCustomerTelegram(ctx context.Context) error {
	customers, err := s.db.Customer.Query().Where(customer.TelegramIDIsNil()).All(ctx)
	if err != nil {
		return err
	}
	for _, c := range customers {
		users, err := customerRwUsers(ctx, s.db, c.ID)
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.TelegramID != nil {
				if err := s.db.Customer.UpdateOneID(c.ID).SetTelegramID(*u.TelegramID).Exec(ctx); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func sameInt64(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
