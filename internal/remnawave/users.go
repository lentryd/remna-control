package remnawave

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Endpoint paths from @remnawave/backend-contract@3.4.15.
const (
	pathUsers            = "/api/users"
	pathUsersStream      = "/api/users/stream"
	pathUserByID         = "/api/users/%d"
	pathUserByUsername   = "/api/users/by-username/%s"
	pathUserEnable       = "/api/users/%d/actions/enable"
	pathUserDisable      = "/api/users/%d/actions/disable"
	pathUserResetTraffic = "/api/users/%d/actions/reset-traffic"
)

const (
	StatusActive   = "ACTIVE"
	StatusDisabled = "DISABLED"
	StatusLimited  = "LIMITED"
	StatusExpired  = "EXPIRED"
)

// User is ExtendedUsersSchema (fields we use).
type User struct {
	ID                   int         `json:"id"`
	ShortUUID            string      `json:"shortUuid"`
	Username             string      `json:"username"`
	Status               string      `json:"status"`
	TrafficLimitBytes    int64       `json:"trafficLimitBytes"`
	TrafficLimitStrategy string      `json:"trafficLimitStrategy"`
	LastTrafficResetAt   *time.Time  `json:"lastTrafficResetAt"`
	ExpireAt             time.Time   `json:"expireAt"`
	TelegramID           *int64      `json:"telegramId"`
	Email                *string     `json:"email"`
	Description          *string     `json:"description"`
	Tag                  *string     `json:"tag"`
	HwidDeviceLimit      *int        `json:"hwidDeviceLimit"`
	SubscriptionURL      string      `json:"subscriptionUrl"`
	CreatedAt            time.Time   `json:"createdAt"`
	ActiveInternalSquads []Squad     `json:"activeInternalSquads"`
	ExternalSquadUUID    *string     `json:"externalSquadUuid"`
	UserTraffic          UserTraffic `json:"userTraffic"`
}

type UserTraffic struct {
	UsedTrafficBytes         int64      `json:"usedTrafficBytes"`
	LifetimeUsedTrafficBytes int64      `json:"lifetimeUsedTrafficBytes"`
	OnlineAt                 *time.Time `json:"onlineAt"`
	FirstConnectedAt         *time.Time `json:"firstConnectedAt"`
	LastConnectedNodeUUID    *string    `json:"lastConnectedNodeUuid"`
}

// SquadUUIDs returns the uuids of the user's active internal squads.
func (u *User) SquadUUIDs() []string {
	out := make([]string, 0, len(u.ActiveInternalSquads))
	for _, s := range u.ActiveInternalSquads {
		out = append(out, s.UUID)
	}
	return out
}

// CreateUserRequest is CreateUserCommand.RequestBodySchema (fields we use).
type CreateUserRequest struct {
	Username             string    `json:"username"`
	ExpireAt             time.Time `json:"expireAt"`
	Status               string    `json:"status,omitempty"`
	TrafficLimitBytes    *int64    `json:"trafficLimitBytes,omitempty"`
	TrafficLimitStrategy string    `json:"trafficLimitStrategy,omitempty"`
	Description          string    `json:"description,omitempty"`
	TelegramID           *int64    `json:"telegramId,omitempty"`
	HwidDeviceLimit      *int      `json:"hwidDeviceLimit,omitempty"`
	ActiveInternalSquads []string  `json:"activeInternalSquads,omitempty"`
	ExternalSquadUUID    *string   `json:"externalSquadUuid,omitempty"`
}

// UpdateUserRequest is UpdateUserCommand.RequestBodySchema: id identifies
// the user, every other field is a partial update.
type UpdateUserRequest struct {
	ID                   int        `json:"id"`
	Status               string     `json:"status,omitempty"`
	ExpireAt             *time.Time `json:"expireAt,omitempty"`
	TrafficLimitBytes    *int64     `json:"trafficLimitBytes,omitempty"`
	TrafficLimitStrategy string     `json:"trafficLimitStrategy,omitempty"`
	Description          *string    `json:"description,omitempty"`
	HwidDeviceLimit      *int       `json:"hwidDeviceLimit,omitempty"`
	ActiveInternalSquads []string   `json:"activeInternalSquads,omitempty"`
	ExternalSquadUUID    *string    `json:"externalSquadUuid,omitempty"`
	TelegramID           *int64     `json:"telegramId,omitempty"`
	// ClearTelegramID sends telegramId: null (omitempty can't express it).
	ClearTelegramID bool `json:"-"`
	// ClearExternalSquad sends externalSquadUuid: null.
	ClearExternalSquad bool `json:"-"`
}

// MarshalJSON adds explicit nulls for telegramId and externalSquadUuid when
// the matching Clear flag is set and the field itself is not.
func (r UpdateUserRequest) MarshalJSON() ([]byte, error) {
	type plain UpdateUserRequest
	clearTG := r.ClearTelegramID && r.TelegramID == nil
	clearSq := r.ClearExternalSquad && r.ExternalSquadUUID == nil
	switch {
	case clearTG && clearSq:
		return json.Marshal(struct {
			plain
			TelegramID        *int64  `json:"telegramId"`
			ExternalSquadUUID *string `json:"externalSquadUuid"`
		}{plain: plain(r)})
	case clearTG:
		return json.Marshal(struct {
			plain
			TelegramID *int64 `json:"telegramId"`
		}{plain: plain(r)})
	case clearSq:
		return json.Marshal(struct {
			plain
			ExternalSquadUUID *string `json:"externalSquadUuid"`
		}{plain: plain(r)})
	}
	return json.Marshal(plain(r))
}

type streamPage struct {
	Users      []User  `json:"users"`
	NextCursor *string `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}

// AllUsers pages through GET /api/users/stream.
func (c *Client) AllUsers(ctx context.Context) ([]User, error) {
	var all []User
	cursor := ""
	for {
		q := url.Values{"size": {"1000"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		page, err := call[streamPage](ctx, c, http.MethodGet, pathUsersStream, q, nil)
		if err != nil {
			return nil, fmt.Errorf("stream users: %w", err)
		}
		all = append(all, page.Users...)
		if !page.HasMore || page.NextCursor == nil || *page.NextCursor == "" || *page.NextCursor == cursor {
			return all, nil
		}
		cursor = *page.NextCursor
	}
}

func (c *Client) GetUser(ctx context.Context, id int) (*User, error) {
	u, err := call[User](ctx, c, http.MethodGet, fmt.Sprintf(pathUserByID, id), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}
	return &u, nil
}

func (c *Client) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u, err := call[User](ctx, c, http.MethodGet, fmt.Sprintf(pathUserByUsername, url.PathEscape(username)), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get user %q: %w", username, err)
	}
	return &u, nil
}

func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	u, err := call[User](ctx, c, http.MethodPost, pathUsers, nil, req)
	if err != nil {
		return nil, fmt.Errorf("create user %q: %w", req.Username, err)
	}
	return &u, nil
}

func (c *Client) UpdateUser(ctx context.Context, req UpdateUserRequest) (*User, error) {
	u, err := call[User](ctx, c, http.MethodPatch, pathUsers, nil, req)
	if err != nil {
		return nil, fmt.Errorf("update user %d: %w", req.ID, err)
	}
	return &u, nil
}

func (c *Client) EnableUser(ctx context.Context, id int) (*User, error) {
	u, err := call[User](ctx, c, http.MethodPost, fmt.Sprintf(pathUserEnable, id), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("enable user %d: %w", id, err)
	}
	return &u, nil
}

func (c *Client) DisableUser(ctx context.Context, id int) (*User, error) {
	u, err := call[User](ctx, c, http.MethodPost, fmt.Sprintf(pathUserDisable, id), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("disable user %d: %w", id, err)
	}
	return &u, nil
}

func (c *Client) ResetTraffic(ctx context.Context, id int) (*User, error) {
	u, err := call[User](ctx, c, http.MethodPost, fmt.Sprintf(pathUserResetTraffic, id), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("reset traffic %d: %w", id, err)
	}
	return &u, nil
}

// itoa is a tiny helper for query values.
func itoa(v int) string { return strconv.Itoa(v) }
