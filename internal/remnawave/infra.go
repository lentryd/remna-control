package remnawave

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	pathAuthLogin       = "/api/auth/login"
	pathInternalSquads  = "/api/internal-squads"
	pathExternalSquads  = "/api/external-squads"
	pathNodes           = "/api/nodes"
	pathStatsNodes      = "/api/bandwidth-stats/nodes"
	pathStatsNodesUsage = "/api/bandwidth-stats/nodes/usage"
)

// Login exchanges admin credentials for a panel JWT (POST /api/auth/login).
// We only use it to verify the credentials; the token is discarded.
func (c *Client) Login(ctx context.Context, username, password string) error {
	var out envelope[struct {
		AccessToken string `json:"accessToken"`
	}]
	body := map[string]string{"username": username, "password": password}
	if err := c.do(ctx, http.MethodPost, pathAuthLogin, nil, body, "", &out); err != nil {
		return err
	}
	if out.Response.AccessToken == "" {
		return fmt.Errorf("panel returned no access token")
	}
	return nil
}

type Squad struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type InternalSquad struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Info struct {
		MembersCount  int `json:"membersCount"`
		InboundsCount int `json:"inboundsCount"`
	} `json:"info"`
}

func (c *Client) InternalSquads(ctx context.Context) ([]InternalSquad, error) {
	out, err := call[struct {
		InternalSquads []InternalSquad `json:"internalSquads"`
	}](ctx, c, http.MethodGet, pathInternalSquads, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list internal squads: %w", err)
	}
	return out.InternalSquads, nil
}

type ExternalSquad struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Info struct {
		MembersCount int `json:"membersCount"`
	} `json:"info"`
}

func (c *Client) ExternalSquads(ctx context.Context) ([]ExternalSquad, error) {
	out, err := call[struct {
		ExternalSquads []ExternalSquad `json:"externalSquads"`
	}](ctx, c, http.MethodGet, pathExternalSquads, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list external squads: %w", err)
	}
	return out.ExternalSquads, nil
}

type Node struct {
	UUID             string `json:"uuid"`
	Name             string `json:"name"`
	Address          string `json:"address"`
	CountryCode      string `json:"countryCode"`
	IsConnected      bool   `json:"isConnected"`
	IsDisabled       bool   `json:"isDisabled"`
	TrafficUsedBytes *int64 `json:"trafficUsedBytes"`
	UsersOnline      int    `json:"usersOnline"`
}

func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	out, err := call[[]Node](ctx, c, http.MethodGet, pathNodes, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return out, nil
}

// NodesUsage is GetStatsNodesUsageCommand's response: daily series per node
// (Categories are the days, aligned with each series' Data).
type NodesUsage struct {
	Categories []string `json:"categories"`
	Series     []struct {
		UUID  string    `json:"uuid"`
		Name  string    `json:"name"`
		Total float64   `json:"total"`
		Data  []float64 `json:"data"`
	} `json:"series"`
}

const dateLayout = "2006-01-02"

// NodesUsageByRange returns per-day traffic of every node between start and
// end (inclusive dates).
func (c *Client) NodesUsageByRange(ctx context.Context, start, end time.Time) (*NodesUsage, error) {
	q := url.Values{
		"start":         {start.Format(dateLayout)},
		"end":           {end.Format(dateLayout)},
		"topNodesLimit": {itoa(1000)},
	}
	out, err := call[NodesUsage](ctx, c, http.MethodGet, pathStatsNodes, q, nil)
	if err != nil {
		return nil, fmt.Errorf("nodes usage: %w", err)
	}
	return &out, nil
}

type UserUsage struct {
	ID         int     `json:"id"`
	TotalBytes float64 `json:"totalBytes"`
}

// NodeUsersUsage returns every user's total traffic on the given nodes over
// the period (GetNodeUsageCommand).
func (c *Client) NodeUsersUsage(ctx context.Context, nodeUUIDs []string, start, end time.Time) (map[string][]UserUsage, error) {
	q := url.Values{
		"start": {start.Format(dateLayout)},
		"end":   {end.Format(dateLayout)},
	}
	out, err := call[struct {
		Nodes []struct {
			UUID  string      `json:"uuid"`
			Users []UserUsage `json:"users"`
		} `json:"nodes"`
	}](ctx, c, http.MethodPost, pathStatsNodesUsage, q, map[string]any{"nodesUuids": nodeUUIDs})
	if err != nil {
		return nil, fmt.Errorf("node users usage: %w", err)
	}
	res := make(map[string][]UserUsage, len(out.Nodes))
	for _, n := range out.Nodes {
		res[n.UUID] = n.Users
	}
	return res, nil
}

const (
	pathInfraProviders    = "/api/infra-billing/providers"
	pathInfraBillingNodes = "/api/infra-billing/nodes"
)

// InfraProvider is a hosting provider from the panel's Infra Billing.
type InfraProvider struct {
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	FaviconLink    *string `json:"faviconLink"`
	LoginURL       *string `json:"loginUrl"`
	BillingHistory struct {
		TotalAmount float64 `json:"totalAmount"`
		TotalBills  int     `json:"totalBills"`
	} `json:"billingHistory"`
}

// InfraProviders lists the panel's infra providers.
func (c *Client) InfraProviders(ctx context.Context) ([]InfraProvider, error) {
	out, err := call[struct {
		Providers []InfraProvider `json:"providers"`
	}](ctx, c, http.MethodGet, pathInfraProviders, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list infra providers: %w", err)
	}
	return out.Providers, nil
}

// InfraBillingNode ties a panel node to a provider and its next payment.
type InfraBillingNode struct {
	UUID          string    `json:"uuid"`
	NodeUUID      *string   `json:"nodeUuid"`
	Name          *string   `json:"name"`
	ProviderUUID  string    `json:"providerUuid"`
	NextBillingAt time.Time `json:"nextBillingAt"`
}

// InfraBillingNodes lists the panel's billing nodes.
func (c *Client) InfraBillingNodes(ctx context.Context) ([]InfraBillingNode, error) {
	out, err := call[struct {
		BillingNodes []InfraBillingNode `json:"billingNodes"`
	}](ctx, c, http.MethodGet, pathInfraBillingNodes, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list infra billing nodes: %w", err)
	}
	return out.BillingNodes, nil
}
