package middleware

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/apitoken"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
)

// ScopeEndpoint is one public API endpoint a token can be allowed.
type ScopeEndpoint struct {
	Key         string `json:"key"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // read | write
	// Company marks the endpoints of a company-bound token: they act on the
	// token's own company. Bound tokens can use nothing else, and unbound
	// tokens can't use these.
	Company bool `json:"company"`
}

// ScopeResource groups endpoints like the panel's API token scopes.
type ScopeResource struct {
	Resource  string          `json:"resource"`
	Endpoints []ScopeEndpoint `json:"endpoints"`
}

// ScopeCatalog is every grantable endpoint; paths are under /api/v1 and
// the routes are wired from it (see api.RegisterRoutes). Scopes follow the
// panel's grammar: "*", "<resource>:*", "<resource>:read",
// "<resource>:write" or an endpoint key.
var ScopeCatalog = []ScopeResource{
	{Resource: "customers", Endpoints: []ScopeEndpoint{
		{Key: "customers:list", Method: "GET", Path: "/api/v1/customers", Description: "List customers", Kind: "read"},
		{Key: "customers:lookup", Method: "GET", Path: "/api/v1/customers/lookup", Description: "Find a customer by ?telegram_id= or ?short_uuid= (subscription short id); same response as customers:get", Kind: "read"},
		{Key: "customers:get", Method: "GET", Path: "/api/v1/customers/{id}", Description: "Customer with balance (balance = balance_own + balance_referral), ledger, subscriptions and payments", Kind: "read"},
		{Key: "customers:create", Method: "POST", Path: "/api/v1/customers", Description: "Create a customer", Kind: "write"},
		{Key: "customers:update", Method: "PUT", Path: "/api/v1/customers/{id}", Description: "Update a customer", Kind: "write"},
		{Key: "customers:delete", Method: "DELETE", Path: "/api/v1/customers/{id}", Description: "Delete (archive) a customer", Kind: "write"},
		{Key: "customers:payment_preview", Method: "POST", Path: "/api/v1/customers/{id}/payments/preview", Description: "Preview a payment: split, referral, resulting balance", Kind: "read"},
		{Key: "customers:pay", Method: "POST", Path: "/api/v1/customers/{id}/payments", Description: "Record a payment (to balance and/or extensions)", Kind: "write"},
		{Key: "customers:adjust", Method: "POST", Path: "/api/v1/customers/{id}/adjust", Description: "Adjust the balance", Kind: "write"},
	}},
	{Resource: "payments", Endpoints: []ScopeEndpoint{
		{Key: "payments:list", Method: "GET", Path: "/api/v1/payments", Description: "List payments", Kind: "read"},
		{Key: "payments:update", Method: "PUT", Path: "/api/v1/payments/{id}", Description: "Update a payment", Kind: "write"},
		{Key: "payments:delete", Method: "DELETE", Path: "/api/v1/payments/{id}", Description: "Delete a payment", Kind: "write"},
	}},
	{Resource: "subscriptions", Endpoints: []ScopeEndpoint{
		{Key: "subscriptions:list", Method: "GET", Path: "/api/v1/subscriptions", Description: "List subscriptions", Kind: "read"},
		{Key: "subscriptions:create", Method: "POST", Path: "/api/v1/subscriptions", Description: "Link a Remnawave user as a subscription", Kind: "write"},
		{Key: "subscriptions:provision", Method: "POST", Path: "/api/v1/subscriptions/provision", Description: "Create a Remnawave user and its subscription", Kind: "write"},
		{Key: "subscriptions:update", Method: "PUT", Path: "/api/v1/subscriptions/{id}", Description: "Update a subscription", Kind: "write"},
		{Key: "subscriptions:delete", Method: "DELETE", Path: "/api/v1/subscriptions/{id}", Description: "Unlink a subscription", Kind: "write"},
		{Key: "subscriptions:connect_addon", Method: "POST", Path: "/api/v1/subscriptions/{id}/addons", Description: "Connect an add-on", Kind: "write"},
		{Key: "subscriptions:update_addon", Method: "PUT", Path: "/api/v1/subscription-addons/{id}", Description: "Update a connected add-on", Kind: "write"},
		{Key: "subscriptions:traffic_anchor", Method: "PUT", Path: "/api/v1/subscriptions/{id}/traffic-anchor", Description: "Set the traffic reset anchor of a subscription", Kind: "write"},
		{Key: "subscriptions:addon_traffic_anchor", Method: "PUT", Path: "/api/v1/subscription-addons/{id}/traffic-anchor", Description: "Set the traffic reset anchor of an add-on", Kind: "write"},
		{Key: "subscriptions:delete_addon", Method: "DELETE", Path: "/api/v1/subscription-addons/{id}", Description: "Unlink a connected add-on", Kind: "write"},
	}},
	{Resource: "items", Endpoints: []ScopeEndpoint{
		{Key: "items:quote", Method: "GET", Path: "/api/v1/items/{kind}/{id}/quote", Description: "Quote an extension (kind: subscription | addon)", Kind: "read"},
		{Key: "items:extend", Method: "POST", Path: "/api/v1/items/{kind}/{id}/extend", Description: "Extend from the balance", Kind: "write"},
		{Key: "items:tariff_quote", Method: "GET", Path: "/api/v1/items/{kind}/{id}/tariff-quote", Description: "Quote a tariff change", Kind: "read"},
		{Key: "items:tariff", Method: "POST", Path: "/api/v1/items/{kind}/{id}/tariff", Description: "Change the tariff", Kind: "write"},
		{Key: "items:enable", Method: "POST", Path: "/api/v1/items/{kind}/{id}/enable", Description: "Enable in Remnawave", Kind: "write"},
		{Key: "items:disable", Method: "POST", Path: "/api/v1/items/{kind}/{id}/disable", Description: "Disable in Remnawave", Kind: "write"},
	}},
	{Resource: "tariffs", Endpoints: []ScopeEndpoint{
		{Key: "tariffs:list", Method: "GET", Path: "/api/v1/tariffs", Description: "List tariffs", Kind: "read"},
		{Key: "tariffs:create", Method: "POST", Path: "/api/v1/tariffs", Description: "Create a tariff", Kind: "write"},
		{Key: "tariffs:update", Method: "PUT", Path: "/api/v1/tariffs/{id}", Description: "Update a tariff", Kind: "write"},
		{Key: "tariffs:delete", Method: "DELETE", Path: "/api/v1/tariffs/{id}", Description: "Delete a tariff", Kind: "write"},
		{Key: "tariffs:sync_included", Method: "POST", Path: "/api/v1/tariffs/{id}/sync-included", Description: "Sync included add-ons to subscriptions", Kind: "write"},
	}},
	{Resource: "addons", Endpoints: []ScopeEndpoint{
		{Key: "addons:list", Method: "GET", Path: "/api/v1/addons", Description: "Add-on catalog (JSON, or addons.yml with ?format=yaml)", Kind: "read"},
		{Key: "addons:full", Method: "GET", Path: "/api/v1/addons/full", Description: "Add-ons with all fields", Kind: "read"},
		{Key: "addons:create", Method: "POST", Path: "/api/v1/addons", Description: "Create an add-on", Kind: "write"},
		{Key: "addons:update", Method: "PUT", Path: "/api/v1/addons/{id}", Description: "Update an add-on", Kind: "write"},
		{Key: "addons:delete", Method: "DELETE", Path: "/api/v1/addons/{id}", Description: "Delete an add-on", Kind: "write"},
	}},
	{Resource: "referrals", Endpoints: []ScopeEndpoint{
		{Key: "referrals:tree", Method: "GET", Path: "/api/v1/referrals/tree", Description: "Referral tree", Kind: "read"},
		{Key: "referrals:accruals", Method: "GET", Path: "/api/v1/referrals/accruals", Description: "Referral accruals", Kind: "read"},
	}},
	{Resource: "expenses", Endpoints: []ScopeEndpoint{
		{Key: "expenses:list", Method: "GET", Path: "/api/v1/expenses", Description: "List expenses", Kind: "read"},
		{Key: "expenses:create", Method: "POST", Path: "/api/v1/expenses", Description: "Create an expense", Kind: "write"},
		{Key: "expenses:update", Method: "PUT", Path: "/api/v1/expenses/{id}", Description: "Update an expense", Kind: "write"},
		{Key: "expenses:delete", Method: "DELETE", Path: "/api/v1/expenses/{id}", Description: "Delete an expense", Kind: "write"},
		{Key: "expenses:providers", Method: "GET", Path: "/api/v1/expenses/providers", Description: "Spending by provider", Kind: "read"},
		{Key: "expenses:items", Method: "GET", Path: "/api/v1/expense-items", Description: "List expense items", Kind: "read"},
		{Key: "expenses:item_create", Method: "POST", Path: "/api/v1/expense-items", Description: "Create an expense item", Kind: "write"},
		{Key: "expenses:item_update", Method: "PUT", Path: "/api/v1/expense-items/{id}", Description: "Update an expense item", Kind: "write"},
		{Key: "expenses:item_delete", Method: "DELETE", Path: "/api/v1/expense-items/{id}", Description: "Delete an expense item", Kind: "write"},
		{Key: "expenses:item_metered", Method: "GET", Path: "/api/v1/expense-items/{id}/metered", Description: "Metered usage of an item", Kind: "read"},
		{Key: "expenses:item_close_period", Method: "POST", Path: "/api/v1/expense-items/{id}/close-period", Description: "Close a metered period", Kind: "write"},
		{Key: "expenses:traffic_sync", Method: "POST", Path: "/api/v1/traffic/sync", Description: "Sync traffic now", Kind: "write"},
	}},
	{Resource: "remnawave", Endpoints: []ScopeEndpoint{
		{Key: "remnawave:users", Method: "GET", Path: "/api/v1/rw/users", Description: "Remnawave users", Kind: "read"},
		{Key: "remnawave:squads", Method: "GET", Path: "/api/v1/rw/squads", Description: "Squads", Kind: "read"},
		{Key: "remnawave:external_squads", Method: "GET", Path: "/api/v1/rw/external-squads", Description: "External squads", Kind: "read"},
		{Key: "remnawave:nodes", Method: "GET", Path: "/api/v1/rw/nodes", Description: "Nodes", Kind: "read"},
		{Key: "remnawave:infra", Method: "GET", Path: "/api/v1/rw/infra", Description: "Infrastructure", Kind: "read"},
		{Key: "remnawave:sync_status", Method: "GET", Path: "/api/v1/rw/sync", Description: "Sync status", Kind: "read"},
		{Key: "remnawave:sync", Method: "POST", Path: "/api/v1/rw/sync", Description: "Sync now", Kind: "write"},
	}},
	{Resource: "stats", Endpoints: []ScopeEndpoint{
		{Key: "stats:dashboard", Method: "GET", Path: "/api/v1/dashboard", Description: "Dashboard figures", Kind: "read"},
		{Key: "stats:fx_rate", Method: "GET", Path: "/api/v1/fx/rate", Description: "Exchange rate", Kind: "read"},
		{Key: "stats:settings", Method: "GET", Path: "/api/v1/settings", Description: "Settings (read only)", Kind: "read"},
		{Key: "stats:audit", Method: "GET", Path: "/api/v1/audit", Description: "Audit log", Kind: "read"},
	}},
	{Resource: "company", Endpoints: []ScopeEndpoint{
		{Key: "company:members", Method: "GET", Path: "/api/v1/company/members", Description: "List the company's employees", Kind: "read", Company: true},
		{Key: "company:member_get", Method: "GET", Path: "/api/v1/company/members/{mid}", Description: "One employee", Kind: "read", Company: true},
		{Key: "company:member_add", Method: "POST", Path: "/api/v1/company/members", Description: "Add an employee (or bring a removed one back); 200 with the existing one if already there", Kind: "write", Company: true},
		{Key: "company:member_update", Method: "PUT", Path: "/api/v1/company/members/{mid}", Description: "Change an employee's label", Kind: "write", Company: true},
		{Key: "company:member_remove", Method: "DELETE", Path: "/api/v1/company/members/{mid}", Description: "Remove an employee (disabled in the panel); protected ones answer 409", Kind: "write", Company: true},
		{Key: "company:balance", Method: "GET", Path: "/api/v1/company/balance", Description: "Balance, accrued so far and the next report date", Kind: "read", Company: true},
		{Key: "company:statements", Method: "GET", Path: "/api/v1/company/statements", Description: "List statements", Kind: "read", Company: true},
		{Key: "company:statement_get", Method: "GET", Path: "/api/v1/company/statements/{sid}", Description: "A statement with its lines", Kind: "read", Company: true},
	}},
	{Resource: "companies", Endpoints: []ScopeEndpoint{
		{Key: "companies:list", Method: "GET", Path: "/api/v1/companies", Description: "List companies", Kind: "read"},
		{Key: "companies:get", Method: "GET", Path: "/api/v1/companies/{id}", Description: "A company", Kind: "read"},
		{Key: "companies:create", Method: "POST", Path: "/api/v1/companies", Description: "Create a company", Kind: "write"},
		{Key: "companies:update", Method: "PUT", Path: "/api/v1/companies/{id}", Description: "Rename, notes and report day", Kind: "write"},
		{Key: "companies:archive", Method: "POST", Path: "/api/v1/companies/{id}/archive", Description: "Archive: final statement, employees removed", Kind: "write"},
		{Key: "companies:set_tariff", Method: "PUT", Path: "/api/v1/companies/{id}/tariff", Description: "Change the company tariff (moves employees that follow it)", Kind: "write"},
		{Key: "companies:members", Method: "GET", Path: "/api/v1/companies/{id}/members", Description: "List employees", Kind: "read"},
		{Key: "companies:member_add", Method: "POST", Path: "/api/v1/companies/{id}/members", Description: "Add an employee", Kind: "write"},
		{Key: "companies:member_update", Method: "PUT", Path: "/api/v1/companies/{id}/members/{mid}", Description: "Change an employee's label", Kind: "write"},
		{Key: "companies:member_remove", Method: "DELETE", Path: "/api/v1/companies/{id}/members/{mid}", Description: "Remove an employee", Kind: "write"},
		{Key: "companies:member_override", Method: "PUT", Path: "/api/v1/companies/{id}/members/{mid}/override", Description: "Set an employee's tariff and per-field overrides", Kind: "write"},
		{Key: "companies:member_protect", Method: "PUT", Path: "/api/v1/companies/{id}/members/{mid}/protect", Description: "Protect or unprotect an employee from removal", Kind: "write"},
		{Key: "companies:balance", Method: "GET", Path: "/api/v1/companies/{id}/balance", Description: "Balance and accrued so far", Kind: "read"},
		{Key: "companies:accrued", Method: "GET", Path: "/api/v1/companies/{id}/accrued", Description: "Accrued and forecast of the open period, by employee", Kind: "read"},
		{Key: "companies:statements", Method: "GET", Path: "/api/v1/companies/{id}/statements", Description: "List statements", Kind: "read"},
		{Key: "companies:statement_get", Method: "GET", Path: "/api/v1/companies/{id}/statements/{sid}", Description: "A statement with its lines", Kind: "read"},
		{Key: "companies:close_period", Method: "POST", Path: "/api/v1/companies/{id}/close-period", Description: "Post the open period now", Kind: "write"},
	}},
	{Resource: "backups", Endpoints: []ScopeEndpoint{
		{Key: "backups:list", Method: "GET", Path: "/api/v1/backups", Description: "List snapshots", Kind: "read"},
		{Key: "backups:download", Method: "GET", Path: "/api/v1/backups/{name}", Description: "Download a snapshot", Kind: "read"},
		{Key: "backups:create", Method: "POST", Path: "/api/v1/backups", Description: "Take a snapshot and download it", Kind: "write"},
	}},
}

func findEndpoint(key string) (string, *ScopeEndpoint) {
	for _, r := range ScopeCatalog {
		for i := range r.Endpoints {
			if r.Endpoints[i].Key == key {
				return r.Resource, &r.Endpoints[i]
			}
		}
	}
	return "", nil
}

// ValidScope reports whether s is a scope of the grammar above.
func ValidScope(s string) bool {
	if s == "*" {
		return true
	}
	if _, ep := findEndpoint(s); ep != nil {
		return true
	}
	res, rest, ok := strings.Cut(s, ":")
	if !ok || (rest != "*" && rest != "read" && rest != "write") {
		return false
	}
	return slices.ContainsFunc(ScopeCatalog, func(r ScopeResource) bool { return r.Resource == res })
}

// Allowed reports whether scopes grant the endpoint key.
func Allowed(scopes []string, key string) bool {
	res, ep := findEndpoint(key)
	if ep == nil {
		return false
	}
	for _, s := range scopes {
		switch s {
		case "*", key, res + ":*", res + ":" + ep.Kind:
			return true
		}
	}
	return false
}

// LocalsToken is the fiber.Ctx.Locals key holding the calling *ent.APIToken.
const LocalsToken = "token"

// LocalsCompanyID holds the company id (int) of a company-bound token.
const LocalsCompanyID = "company_id"

// companyScope reports whether s is "company:*|read|write" or the key of a
// company endpoint.
func companyScope(s string) bool {
	if res, ep := findEndpoint(s); ep != nil {
		return ep.Company && res == "company"
	}
	res, rest, ok := strings.Cut(s, ":")
	return ok && res == "company" && (rest == "*" || rest == "read" || rest == "write")
}

// GrantsCompany reports whether the scopes name company endpoints, which only
// a company-bound token can use. "*" doesn't count.
func GrantsCompany(scopes []string) bool { return slices.ContainsFunc(scopes, companyScope) }

// CompanyScopesOnly reports whether every scope is "*" or a company scope,
// the only ones a company-bound token may carry.
func CompanyScopesOnly(scopes []string) bool {
	return !slices.ContainsFunc(scopes, func(s string) bool { return s != "*" && !companyScope(s) })
}

const tokenPrefix = "vpc_"

// NewToken returns a fresh API token and the hash to store.
func NewToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is how tokens are stored and looked up.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RequireToken accepts "Authorization: Bearer <token>" of an unexpired
// token whose scopes allow the endpoint key.
func RequireToken(db *ent.Client, key string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
		if !ok || raw == "" {
			return apperr.Status(fiber.StatusUnauthorized, "token.required", "API token required")
		}
		ctx := c.UserContext()
		t, err := db.APIToken.Query().Where(apitoken.TokenHash(HashToken(strings.TrimSpace(raw)))).Only(ctx)
		if err != nil {
			return apperr.Status(fiber.StatusUnauthorized, "token.invalid", "invalid API token")
		}
		if t.ExpireAt != nil && t.ExpireAt.Before(time.Now()) {
			return apperr.Status(fiber.StatusUnauthorized, "token.expired", "API token expired")
		}
		if !Allowed(t.Scopes, key) {
			return apperr.Status(fiber.StatusForbidden, "token.scope", "token lacks scope {{scope}}", "scope", key)
		}
		// A company-bound token reaches company endpoints only, and only
		// those: whatever scopes it carries.
		_, ep := findEndpoint(key)
		switch {
		case t.CompanyID != nil && !ep.Company:
			return apperr.Status(fiber.StatusForbidden, "token.company_bound", "a company token can't use this endpoint")
		case t.CompanyID == nil && ep.Company:
			return apperr.Status(fiber.StatusForbidden, "token.company_required", "this endpoint needs a token bound to a company")
		}
		// Coarse last-use tracking: one write a minute at most.
		if t.LastUsedAt == nil || time.Since(*t.LastUsedAt) > time.Minute {
			_ = db.APIToken.UpdateOne(t).SetLastUsedAt(time.Now()).Exec(ctx)
		}
		c.Locals(LocalsToken, t)
		if t.CompanyID != nil {
			c.Locals(LocalsCompanyID, *t.CompanyID)
		}
		c.SetUserContext(audit.WithActor(ctx, "token:"+t.Name))
		return c.Next()
	}
}
