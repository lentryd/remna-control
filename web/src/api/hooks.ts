import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from './client'
import type {
    Accrual,
    Addon,
    AuditRow,
    Customer,
    CustomerDetail,
    Dashboard,
    Expense,
    ExpenseItem,
    Job,
    JobRun,
    MeteredSummary,
    Payment,
    ProviderTotals,
    ReferralNode,
    RwNode,
    RwUserRow,
    Squad,
    Subscription,
    Tariff,
    Infra
} from './types'

export const qk = {
    me: ['me'],
    dashboard: ['dashboard'],
    customers: ['customers'],
    customer: (id: number) => ['customers', id],
    subscriptions: ['subscriptions'],
    payments: ['payments'],
    tariffs: ['tariffs'],
    addons: ['addons'],
    rwUsers: ['rw-users'],
    squads: ['squads'],
    nodes: ['nodes'],
    expenses: ['expenses'],
    providers: ['expenses', 'providers'],
    expenseItems: ['expense-items'],
    metered: (id: number, month: string) => ['metered', id, month],
    referralTree: ['referrals', 'tree'],
    accruals: ['referrals', 'accruals'],
    settings: ['settings'],
    audit: ['audit'],
    jobs: ['jobs'],
    jobRuns: (key: string) => ['jobs', key, 'runs']
}

export const useMe = () =>
    useQuery({ queryKey: qk.me, queryFn: () => api.get<{ username: string }>('auth/me'), retry: false, staleTime: 60_000 })
export const useDashboard = () => useQuery({ queryKey: qk.dashboard, queryFn: () => api.get<Dashboard>('dashboard') })
export const useCustomers = (enabled = true) =>
    useQuery({ queryKey: qk.customers, queryFn: () => api.get<Customer[]>('customers'), enabled })
export const useCustomer = (id: number) =>
    useQuery({ queryKey: qk.customer(id), queryFn: () => api.get<CustomerDetail>(`customers/${id}`), enabled: id > 0 })
export const useSubscriptions = (enabled = true) =>
    useQuery({ queryKey: qk.subscriptions, queryFn: () => api.get<Subscription[]>('subscriptions'), enabled })
export const usePayments = () => useQuery({ queryKey: qk.payments, queryFn: () => api.get<Payment[]>('payments') })
export const useTariffs = () => useQuery({ queryKey: qk.tariffs, queryFn: () => api.get<Tariff[]>('tariffs') })
export const useAddons = () => useQuery({ queryKey: qk.addons, queryFn: () => api.get<Addon[]>('addons') })
export const useRwUsers = () => useQuery({ queryKey: qk.rwUsers, queryFn: () => api.get<RwUserRow[]>('rw/users') })
export const useSquads = () =>
    useQuery({ queryKey: qk.squads, queryFn: () => api.get<Squad[]>('rw/squads'), staleTime: 300_000 })
export const useInfra = () =>
    useQuery({ queryKey: ['rw', 'infra'], queryFn: () => api.get<Infra>('rw/infra'), staleTime: 300_000 })
export const useNodes = () =>
    useQuery({ queryKey: qk.nodes, queryFn: () => api.get<RwNode[]>('rw/nodes'), staleTime: 300_000 })
export const useExpenses = () => useQuery({ queryKey: qk.expenses, queryFn: () => api.get<Expense[]>('expenses') })
export const useProviders = () =>
    useQuery({ queryKey: qk.providers, queryFn: () => api.get<ProviderTotals[]>('expenses/providers') })
export const useExpenseItems = () =>
    useQuery({
        queryKey: qk.expenseItems,
        queryFn: () => api.get<{ items: ExpenseItem[]; monthly_total: number }>('expense-items')
    })
export const useMetered = (id: number, month: string) =>
    useQuery({
        queryKey: qk.metered(id, month),
        queryFn: () => api.get<MeteredSummary>(`expense-items/${id}/metered?month=${month}`),
        enabled: id > 0
    })
export const useReferralTree = () =>
    useQuery({ queryKey: qk.referralTree, queryFn: () => api.get<ReferralNode[]>('referrals/tree') })
export const useAccruals = () => useQuery({ queryKey: qk.accruals, queryFn: () => api.get<Accrual[]>('referrals/accruals') })
export const useSettings = (enabled = true) =>
    useQuery({ queryKey: qk.settings, queryFn: () => api.get<Record<string, string>>('settings'), enabled })
export const useAudit = () => useQuery({ queryKey: qk.audit, queryFn: () => api.get<AuditRow[]>('audit') })
// useJobs polls the background jobs list every refetchMs while it is used.
export const useJobs = (refetchMs: number) =>
    useQuery({ queryKey: qk.jobs, queryFn: () => api.get<Job[]>('jobs'), refetchInterval: refetchMs })
// useJobRuns loads one job's history; enabled keeps it off until the row is open.
export const useJobRuns = (key: string, enabled: boolean) =>
    useQuery({
        queryKey: qk.jobRuns(key),
        queryFn: () => api.get<JobRun[]>(`jobs/${key}/runs`),
        enabled,
        refetchInterval: 10_000
    })

// useInvalidateAll refreshes every query after a mutation: data here is
// small and heavily cross-linked (a payment changes balances, expiries,
// dashboard and referral numbers at once).
export function useInvalidateAll() {
    const qc = useQueryClient()
    return () => qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== 'me' })
}

// useApiMutation wraps a request with global invalidation.
export function useApiMutation<TVars, TRes = unknown>(fn: (vars: TVars) => Promise<TRes>) {
    const invalidate = useInvalidateAll()
    return useMutation({ mutationFn: fn, onSuccess: () => invalidate() })
}
