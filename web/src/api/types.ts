export type RwStatus = 'ACTIVE' | 'DISABLED' | 'LIMITED' | 'EXPIRED' | string

export interface RwUser {
    id: number
    username: string
    short_uuid: string
    status: RwStatus
    expire_at: string | null
    unlimited: boolean
    used_traffic_bytes: number
    traffic_limit_bytes: number
    traffic_limit_strategy: string
    last_traffic_reset_at: string | null
    // When the panel resets traffic next (predicted from the strategy); null if never.
    next_traffic_reset_at: string | null
    hwid_device_limit: number | null
    online_at: string | null
    description: string
    tag: string
    telegram_id: number | null
    subscription_url: string
    squad_uuids: string[] | null
    deleted: boolean
    synced_at: string
}

export interface RwUserRow extends RwUser {
    subscription_id: number | null
    subscription_addon_id: number | null
    parent_subscription_id: number | null
    customer_id: number | null
    customer_name: string
    linked: boolean
}

export interface AddonItem {
    id: number
    subscription_id: number
    addon_id: number
    addon_name: string
    tariff_id: number | null
    tariff_name: string
    price_override: number | null
    price: number
    auto_extend: boolean
    // included add-ons come with the subscription's tariff: free, extended with it
    included: boolean
    // non-null only while traffic resets by subscription
    traffic_anchor_at: string | null
    rw_user: RwUser | null
}

export interface Subscription {
    id: number
    customer_id: number
    customer_name: string
    customer_archived: boolean
    tariff_id: number | null
    tariff_name: string
    label: string
    title: string
    price_override: number | null
    price: number
    auto_extend: boolean
    // non-null only while traffic resets by subscription
    traffic_anchor_at: string | null
    rw_user: RwUser | null
    addons: AddonItem[]
    created_at: string
}

export interface Customer {
    id: number
    name: string
    contact: string
    telegram_id: number | null
    notes: string
    referrer_id: number | null
    referrer_name: string
    referral_percent: number | null
    archived: boolean
    balance: number
    balance_own: number
    balance_referral: number
    subscriptions_count: number
    addons_count: number
    monthly: number
    nearest_expire_at: string | null
    referrals_count: number
    total_paid: number
    last_payment_at: string | null
    created_at: string
}

export interface Payment {
    id: number
    customer_id: number
    customer_name: string
    amount: number
    date: string
    method: string
    note: string
    historical: boolean
}

export interface LedgerEntry {
    id: number
    type: 'payment' | 'charge' | 'adjustment' | 'refund' | 'referral'
    amount: number
    date: string
    note: string
}

export interface Accrual {
    id: number
    referrer_id: number
    referrer_name: string
    referee_id: number
    referee_name: string
    payment_id: number
    percent: number
    amount: number
    date: string
    status: string
}

export interface Extension {
    id: number
    kind: 'extend' | 'connect' | 'tariff_change'
    months: number
    days: number
    amount: number
    from_at: string | null
    to_at: string | null
    status: 'ok' | 'failed'
    error: string
    actor: string
    created_at: string
    subscription_id: number | null
    subscription_addon_id: number | null
}

export interface CustomerDetail extends Customer {
    subscriptions: Subscription[]
    payments: Payment[] | null
    ledger: LedgerEntry[] | null
    referrals: Customer[] | null
    accruals: Accrual[] | null
    extensions: Extension[] | null
}

export interface Period {
    months: number
    days: number
    price: number
}

export interface Tariff {
    id: number
    kind: 'base' | 'addon'
    addon_id: number | null
    addon_name: string
    name: string
    description: string
    monthly_price: number
    active: boolean
    sort_order: number
    manage_rw: boolean
    traffic_limit_bytes: number
    traffic_strategy: string
    hwid_limit: number | null
    squad_uuids: string[]
    periods: Period[]
    included_addon_tariff_ids: number[]
    subscribers: number
    overridden: number
    mrr: number
}

// AddonChange is what a tariff switch does to an included add-on.
export interface AddonChange {
    subscription_addon_id: number | null
    addon_id: number
    addon_name: string
    tariff_name: string
    action: 'connect' | 'include' | 'remove'
    credit: number
}

export interface TariffQuote {
    old_monthly: number
    new_monthly: number
    expire_at: string | null
    surcharge: number
    addons: AddonChange[]
}

export interface Addon {
    id: number
    name: string
    prefix: string
    suffix: string
    // file add-ons come from ADDONS_CONFIG and are read-only here
    in_config: boolean
    source: 'file' | 'ui'
    remark: string
    remark_unlimited: string
    stubs: Record<string, string>
}

export interface ApiToken {
    id: number
    uuid: string
    name: string
    prefix: string
    scopes: string[]
    created_at: string
    // null: made before tokens expired, never expires
    expire_at: string | null
    last_used_at: string | null
    // only in the creation response
    token?: string
}

export interface Squad {
    uuid: string
    name: string
    info: { membersCount: number; inboundsCount: number }
}

export interface RwNode {
    uuid: string
    name: string
    address: string
    countryCode: string
    isConnected: boolean
    isDisabled: boolean
    usersOnline: number
}

export interface Expense {
    id: number
    date: string
    provider: string
    provider_uuid: string
    expense_item_id: number | null
    item_name: string
    kind: 'charge' | 'refund'
    orig_amount: number
    orig_currency: string
    fx_rate: number
    fee_percent: number
    share_percent: number
    rub_amount: number
    refund_of_id: number | null
    refunded_total: number
    calc_rub_amount: number | null
    metered_gb: number | null
    period: string
    note: string
}

export interface ProviderTotals {
    provider: string
    provider_uuid: string
    gross: number
    refunded: number
    net: number
    count: number
    last: string
}

export type GBUnit = 'binary' | 'decimal'
export type MinMode = 'floor' | 'free'

// Tier is a graduated price step: GB up to up_to_gb (0 = unlimited).
export interface Tier {
    up_to_gb: number
    price_per_gb: number
}

export interface ExpenseItem {
    id: number
    name: string
    provider: string
    provider_uuid: string
    currency: string
    pricing: 'fixed' | 'metered'
    amount: number
    period: 'month' | 'year'
    fee_percent: number
    share_percent: number
    price_per_gb: number
    min_charge: number
    gb_unit: GBUnit
    min_mode: MinMode
    free_gb: number
    tiers: Tier[]
    billing_day: number
    rw_node_uuid: string
    rw_squad_uuid: string
    next_due_date: string | null
    active: boolean
    notes: string
    monthly_rub: number | null
    rate: number | null
    plan_error?: string
    plan_error_code?: string
    plan_error_params?: Record<string, unknown>
}

export interface Consumer {
    rw_user_id: number
    username: string
    customer_id: number | null
    customer_name: string
    subscription_id: number | null
    sub_title: string
    gb: number
    share_percent: number
    cost_rub: number
}

export interface MeteredSummary {
    item_id: number
    name: string
    node_uuid: string
    node_name: string
    squad_uuid: string
    squad_share_percent: number
    period: string
    period_start: string
    period_end: string
    currency: string
    gb_unit: GBUnit
    min_mode: MinMode
    free_gb: number
    tiers: Tier[]
    price_per_gb: number
    min_charge: number
    included_gb: number
    used_gb: number
    cost: number
    cost_rub: number
    forecast_gb: number
    forecast_cost: number
    forecast_rub: number
    daily: { date: string; gb: number }[]
    top_consumers: Consumer[]
    consumer_error: string
    error?: string
    error_code?: string
    error_params?: Record<string, unknown>
}

export interface ExpiringItem {
    kind: 'subscription' | 'addon'
    id: number
    subscription_id: number
    title: string
    customer_id: number
    customer_name: string
    status: RwStatus
    expire_at: string
    days_left: number
    price: number
    balance: number
}

// TrafficItem is a panel user running out of its traffic limit.
export interface TrafficItem {
    kind: 'subscription' | 'addon'
    id: number
    subscription_id: number
    title: string
    customer_id: number
    customer_name: string
    status: RwStatus
    used_traffic_bytes: number
    traffic_limit_bytes: number
    traffic_limit_strategy: string
    used_pct: number
    next_traffic_reset_at: string | null
    last_traffic_reset_at: string | null
}

export interface Dashboard {
    window_days: number
    mrr: number
    planned_expenses: number
    profit: number
    active_customers: number
    active_subs: number
    active_addons: number
    income_total: number
    expenses_total: number
    cash_balance: number
    income_month: number
    expenses_month: number
    refunds_month: number
    referral_total: number
    referral_month: number
    debt_total: number
    expiring: ExpiringItem[]
    traffic_low: TrafficItem[]
    traffic_stats: { ok: number; low: number; limited: number; unlimited: number }
    due_soon:
        | {
              id: number
              name: string
              provider: string
              provider_uuid?: string
              node_uuid?: string
              next_due_date: string
              monthly_rub?: number
              source: 'item' | 'panel'
          }[]
        | null
    metered: MeteredSummary[] | null
    months: { month: string; income: number; expenses: number; refunds: number }[]
    sync: { last_sync: string | null; error: string | null }
}

export interface ReferralNode {
    id: number
    name: string
    archived: boolean
    monthly: number
    total_paid: number
    direct_count: number
    direct_active: number
    branch_count: number
    branch_paid: number
    branch_monthly: number
    accrued_total: number
    children: ReferralNode[]
}

export interface Allocation {
    kind: 'subscription' | 'addon'
    id: number
    title: string
    months: number
    days: number
    amount: number
    from: string
    to: string
}

export interface PlanItem {
    kind: 'subscription' | 'addon'
    id: number
    title: string
    monthly: number
    expire_at: string | null
    periods: Period[] | null
    // a payment is spread over auto-extended items; the rest are manual
    auto_extend: boolean
}

export interface ReferralInfo {
    referrer_id: number
    referrer_name: string
    percent: number
    amount: number
}

export interface PaymentPreview {
    balance_before: number
    balance_after_payment: number
    allocations: Allocation[]
    remainder: number
    items: PlanItem[]
    referral: ReferralInfo | null
}

export interface ExtensionResult {
    kind: string
    id: number
    title: string
    months: number
    days: number
    amount: number
    from: string | null
    to: string | null
    ok: boolean
    error: string
}

// Job is a background job with its persisted status. Times are RFC3339;
// last_ok null means it never ran, interval_seconds null means the schedule is off.
export interface Job {
    key: string
    interval_seconds: number | null
    enabled: boolean
    running: boolean
    last_run_at: string | null
    last_success_at: string | null
    last_failure_at: string | null
    last_ok: boolean | null
    last_duration_ms: number
    last_summary: string
    last_error: string
    runs: number
    failures: number
}

// JobRun is one history row of a job; for repeat > 1 started_at is the
// first occurrence of the same failure and finished_at the last.
export interface JobRun {
    id: number
    started_at: string
    finished_at: string
    duration_ms: number
    ok: boolean
    text: string
    repeat: number
}

export interface AuditRow {
    id: number
    at: string
    actor: string
    action: string
    entity: string
    entity_id: number
    payload: string
    ok: boolean
    error: string
}

export interface InfraProvider {
    uuid: string
    name: string
    faviconLink: string | null
    loginUrl: string | null
    billingHistory: { totalAmount: number; totalBills: number }
}

export interface InfraBillingNode {
    uuid: string
    nodeUuid: string | null
    name: string | null
    providerUuid: string
    nextBillingAt: string
}

export interface Infra {
    providers: InfraProvider[]
    billing_nodes: InfraBillingNode[]
    error: string | null
}
