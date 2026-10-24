import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { BarChart } from '@mantine/charts'
import { ActionIcon, Alert, Anchor, Badge, Box, Button, Card, Group, RingProgress, SimpleGrid, Stack, Text, Tooltip } from '@mantine/core'
import {
    IconAffiliate,
    IconCalendarDue,
    IconCalendarPlus,
    IconChartBar,
    IconChartDonut3,
    IconChartLine,
    IconClockExclamation,
    IconCoins,
    IconCreditCard,
    IconGauge,
    IconReceipt2,
    IconUserPlus,
    IconWallet,
    IconWifiOff
} from '@tabler/icons-react'
import dayjs from 'dayjs'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router'

import { useDashboard } from '@/api/hooks'
import type { Dashboard, ExpiringItem, TrafficItem } from '@/api/types'
import { cssColor, Dot, expiryColor, ExpireCell, StatusPill, trafficColor, trafficHint, trafficResetColor, TrafficMini, trafficResetText } from '@/components/badges'
import { fmtDate, fmtMoney } from '@/components/format'
import { MeteredCard } from '@/components/MeteredCard'
import { Money, PageHeader, StatCard } from '@/components/ui'
import { openCustomerForm } from '@/modals/CustomerModals'
import { openExtendModal } from '@/modals/ExtendModal'
import { openExpenseForm } from '@/modals/ExpenseModal'
import { openPaymentModal } from '@/modals/PaymentModal'
import { openViewAddonModal, openViewSubscriptionModal } from '@/modals/ViewItemModal'
import { NodeLabel } from '@shared/ui/infra/node'
import { ProviderLabel } from '@shared/ui/infra/provider'
import { LoadingScreen } from '@shared/ui/loading-screen'
import { Page } from '@shared/ui/page'
import { DataTableCard, DataTableShared } from '@shared/ui/table'

// Block is a dashboard card with a header row.
function Block({
    icon,
    title,
    description,
    actions,
    children,
    flush,
    style
}: {
    icon?: React.ReactNode
    title: string
    description?: React.ReactNode
    actions?: React.ReactNode
    children: React.ReactNode
    flush?: boolean
    style?: React.CSSProperties
}) {
    return (
        <DataTableShared.Container style={style}>
            <DataTableShared.Title actions={actions} description={description} icon={icon} title={title} />
            {flush ? children : <Card.Section p="lg">{children}</Card.Section>}
        </DataTableShared.Container>
    )
}

// Blocks rise in one after another.
const rise = (index: number, className?: string) => ({
    className: className ? `remnac-enter ${className}` : 'remnac-enter',
    style: { animationDelay: `${index * 40}ms` }
})

const openItem = (e: { kind: 'subscription' | 'addon'; id: number; subscription_id: number; title: string; customer_name: string }) =>
    e.kind === 'addon' ? openViewAddonModal(e.id, e.subscription_id) : openViewSubscriptionModal({ id: e.id, title: e.title, customer_name: e.customer_name })

export function DashboardPage() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const { data, isPending, error } = useDashboard()
    if (isPending) return <LoadingScreen height="60vh" />
    if (error || !data) return <Alert color="red">{error?.message}</Alert>

    const today = dayjs().format('dddd, D MMMM')
    const cards = [
        {
            title: t('dashboard.mrr'),
            value: fmtMoney(data.mrr),
            hint: t('dashboard.mrr_hint', { customers: data.active_customers, subs: data.active_subs, addons: data.active_addons }),
            icon: IconCoins,
            color: 'brand'
        },
        { title: t('dashboard.expenses_month'), value: fmtMoney(data.planned_expenses), hint: t('dashboard.expenses_hint'), icon: IconReceipt2, color: 'orange' },
        {
            title: t('dashboard.profit_month'),
            value: fmtMoney(data.profit),
            hint: t('dashboard.profit_hint'),
            icon: IconChartLine,
            color: data.profit >= 0 ? 'teal' : 'red'
        },
        {
            title: t('dashboard.cash'),
            value: fmtMoney(data.cash_balance),
            hint: t(data.refunds_month > 0 ? 'dashboard.cash_hint_refunds' : 'dashboard.cash_hint', {
                income: fmtMoney(data.income_month),
                expenses: fmtMoney(data.expenses_month),
                refunds: fmtMoney(data.refunds_month)
            }),
            icon: IconWallet,
            color: 'blue'
        }
    ]

    return (
        <Page title={t('menu.home')}>
            <PageHeader
                actions={
                    <>
                        <Button leftSection={<IconCreditCard size={16} />} onClick={() => openPaymentModal()} variant="default">
                            {t('customer.record_payment')}
                        </Button>
                        <Button
                            leftSection={<IconUserPlus size={16} />}
                            onClick={() => openCustomerForm(undefined, (id) => navigate(`/customers/${id}`))}
                            variant="filled"
                        >
                            {t('dashboard.new_customer')}
                        </Button>
                    </>
                }
                description={today.charAt(0).toUpperCase() + today.slice(1)}
                icon={null}
                title={t('menu.home')}
            />
            {data.sync.error && (
                <Alert color="red" icon={<IconWifiOff size={18} />} mb="lg" title={t('dashboard.no_panel')}>
                    {data.sync.error}
                </Alert>
            )}

            <SimpleGrid cols={{ base: 1, xs: 2, xl: 4 }} mb="lg" spacing="md">
                {cards.map((c, i) => (
                    <div key={c.title} {...rise(i)}>
                        <StatCard color={c.color} hint={c.hint} icon={c.icon} title={c.title} value={c.value} />
                    </div>
                ))}
            </SimpleGrid>

            <Box className="dash-grid" mb="lg">
                <div {...rise(4, 'dash-wide')}>
                    <MonthsChart months={data.months} />
                </div>
                <div {...rise(5)}>{data.traffic_stats && <TrafficStatsBlock items={data.traffic_low ?? []} stats={data.traffic_stats} />}</div>
            </Box>

            <Box className="dash-grid" mb="lg">
                <Stack className="dash-wide" gap="lg">
                    <ExpiringTable data={data.expiring} windowDays={data.window_days} />
                    <TrafficLowTable data={data.traffic_low ?? []} />
                </Stack>
                <Stack gap="lg">
                    <Block description={t('dashboard.referrals_hint')} icon={<IconAffiliate />} title={t('dashboard.referrals')}>
                        <Stack gap={10}>
                            <Row label={t('dashboard.accrued_month')} value={<Money value={data.referral_month} />} />
                            <Row label={t('dashboard.total')} value={<Money value={data.referral_total} />} />
                            {data.debt_total > 0 && <Row label={<Text c="red" inherit>{t('dashboard.debts')}</Text>} value={<Money value={data.debt_total} />} />}
                        </Stack>
                    </Block>
                    <DueSoon items={data.due_soon} />
                </Stack>
            </Box>

            {data.metered?.map((m) => <MeteredCard key={m.item_id} m={m} />)}
        </Page>
    )
}

function Row({ label, value }: { label: React.ReactNode; value: React.ReactNode }) {
    return (
        <Group gap="sm" justify="space-between" wrap="nowrap">
            <Text c="dimmed" size="sm">
                {label}
            </Text>
            <Text className="num" fw={600} size="sm">
                {value}
            </Text>
        </Group>
    )
}

// MonthsChart compares money in and out per month, with the period's totals.
function MonthsChart({ months }: { months: Dashboard['months'] }) {
    const { t } = useTranslation()
    const income = months.reduce((s, m) => s + m.income, 0)
    const spent = months.reduce((s, m) => s + m.expenses, 0)
    const refunds = months.reduce((s, m) => s + m.refunds, 0)
    const net = income - spent + refunds
    const legend = [
        { label: t('dashboard.period_income'), value: income, color: 'var(--chart-income)' },
        { label: t('dashboard.period_spent'), value: spent, color: 'var(--chart-expense)' },
        ...(refunds > 0 ? [{ label: t('dashboard.period_refunds'), value: refunds, color: 'var(--chart-refund)' }] : [])
    ]
    return (
        <Block description={t('dashboard.last_months', { count: months.length })} icon={<IconChartBar />} style={{ height: '100%' }} title={t('dashboard.by_month')}>
            <Group gap="xl" mb="lg" wrap="wrap">
                {legend.map((l) => (
                    <Box key={l.label}>
                        <Group gap={6} wrap="nowrap">
                            <Dot color={l.color} size={8} />
                            <Text c="dimmed" fw={500} size="xs">
                                {l.label}
                            </Text>
                        </Group>
                        <Text className="num" fw={600} fz={20} mt={2} style={{ letterSpacing: '-0.02em' }}>
                            {fmtMoney(l.value)}
                        </Text>
                    </Box>
                ))}
                <Box>
                    <Text c="dimmed" fw={500} size="xs">
                        {t('dashboard.period_net')}
                    </Text>
                    <Text c={net >= 0 ? 'teal' : 'red'} className="num" fw={600} fz={20} mt={2} style={{ letterSpacing: '-0.02em' }}>
                        {net > 0 ? '+' : ''}
                        {fmtMoney(net)}
                    </Text>
                </Box>
            </Group>
            <BarChart
                barChartProps={{ barGap: 2, barCategoryGap: '28%' }}
                data={months.map((m) => ({ ...m, month: dayjs(m.month + '-01').format('MMM YY') }))}
                dataKey="month"
                gridAxis="x"
                strokeDasharray="0"
                gridProps={{ stroke: 'var(--chart-grid)' }}
                h={250}
                series={[
                    { name: 'income', label: t('dashboard.income'), color: 'var(--chart-income)' },
                    { name: 'expenses', label: t('dashboard.spent'), color: 'var(--chart-expense)' },
                    ...(refunds > 0 ? [{ name: 'refunds', label: t('dashboard.refunds'), color: 'var(--chart-refund)' }] : [])
                ]}
                tickLine="none"
                valueFormatter={(v) => fmtMoney(v)}
                yAxisProps={{ width: 64, tickFormatter: (v: number) => (Math.abs(v) >= 1000 ? `${Math.round(v / 1000)}k` : String(v)) }}
            />
        </Block>
    )
}

function DueSoon({ items }: { items: Dashboard['due_soon'] }) {
    const { t } = useTranslation()
    return (
        <Block icon={<IconCalendarDue />} title={t('dashboard.due_soon')}>
            {items?.length ? (
                <Stack gap="md">
                    {items.map((d, i) => {
                        const days = dayjs(d.next_due_date).diff(dayjs(), 'day')
                        return (
                            <Group gap="sm" justify="space-between" key={`${d.id}-${i}`} wrap="nowrap">
                                <Stack gap={4} miw={0}>
                                    <Group gap="xs" wrap="nowrap">
                                        <Text fw={500} size="sm" truncate="end">
                                            {d.name}
                                        </Text>
                                        {d.source === 'panel' && (
                                            <Badge color="gray" size="xs">
                                                {t('dashboard.panel_badge')}
                                            </Badge>
                                        )}
                                    </Group>
                                    <Group gap="sm" wrap="nowrap">
                                        {d.provider && <ProviderLabel name={d.provider} size="xs" uuid={d.provider_uuid} />}
                                        {d.node_uuid && <NodeLabel size="xs" uuid={d.node_uuid} />}
                                    </Group>
                                </Stack>
                                <Group gap="xs" wrap="nowrap">
                                    <Stack align="flex-end" gap={2}>
                                        <Badge color={expiryColor(days)} size="md">
                                            {fmtDate(d.next_due_date)}
                                        </Badge>
                                        {d.monthly_rub !== undefined && (
                                            <Text c="dimmed" className="num" size="xs">
                                                {fmtMoney(d.monthly_rub)}
                                            </Text>
                                        )}
                                    </Stack>
                                    <Tooltip label={t('expense_items.pay')}>
                                        <ActionIcon
                                            aria-label={t('expense_items.pay')}
                                            onClick={() =>
                                                openExpenseForm({
                                                    itemId: d.id || undefined,
                                                    name: d.name,
                                                    provider: d.provider ? { name: d.provider, uuid: d.provider_uuid } : undefined
                                                })
                                            }
                                            size="lg"
                                            variant="default"
                                        >
                                            <IconReceipt2 size={16} />
                                        </ActionIcon>
                                    </Tooltip>
                                </Group>
                            </Group>
                        )
                    })}
                </Stack>
            ) : (
                <Text c="dimmed" size="sm">
                    {t('dashboard.no_due')}{' '}
                    <Anchor component={Link} size="sm" to="/expense-items">
                        {t('dashboard.no_due_link')}
                    </Anchor>
                </Text>
            )}
        </Block>
    )
}

function TitleCell({ kind, title, customerId, customerName, color }: { kind: string; title: string; customerId: number; customerName: string; color: string }) {
    const { t } = useTranslation()
    return (
        <Group gap="sm" wrap="nowrap">
            <Dot color={color} size={8} />
            <Box miw={0}>
                <Group gap={6} wrap="nowrap">
                    <Text fw={500} size="sm" truncate="end">
                        {title}
                    </Text>
                    {kind === 'addon' && (
                        <Badge color="grape" size="xs">
                            {t('dashboard.addon_badge')}
                        </Badge>
                    )}
                </Group>
                <Anchor c="dimmed" component={Link} size="xs" to={`/customers/${customerId}`}>
                    {customerName}
                </Anchor>
            </Box>
        </Group>
    )
}

function ExpiringTable({ data, windowDays }: { data: ExpiringItem[]; windowDays: number }) {
    const { t } = useTranslation()
    const columns = useMemo<MRT_ColumnDef<ExpiringItem>[]>(
        () => [
            {
                accessorKey: 'title',
                header: t('dashboard.col_subscription'),
                size: 220,
                Cell: ({ row }) => (
                    <TitleCell
                        color={expiryColor(row.original.days_left)}
                        customerId={row.original.customer_id}
                        customerName={row.original.customer_name}
                        kind={row.original.kind}
                        title={row.original.title}
                    />
                )
            },
            {
                accessorKey: 'days_left',
                header: t('dashboard.col_term'),
                size: 150,
                Cell: ({ row }) => <ExpireCell align="flex-start" date={row.original.expire_at} />
            },
            {
                accessorKey: 'status',
                header: t('dashboard.col_status'),
                size: 130,
                Cell: ({ row }) => <StatusPill status={row.original.status} />
            },
            {
                accessorKey: 'price',
                header: t('dashboard.col_price'),
                size: 100,
                mantineTableHeadCellProps: { align: 'right' },
                mantineTableBodyCellProps: { align: 'right' },
                Cell: ({ cell }) => <Money value={cell.getValue<number>()} />
            },
            {
                accessorKey: 'balance',
                header: t('dashboard.col_balance'),
                size: 100,
                mantineTableHeadCellProps: { align: 'right' },
                mantineTableBodyCellProps: { align: 'right' },
                Cell: ({ cell }) => <Money signed value={cell.getValue<number>()} />
            }
        ],
        [t]
    )
    return (
        <DataTableCard
            actions={<Badge color={data.length ? 'orange' : 'teal'} size="lg">{data.length}</Badge>}
            columns={columns}
            compact
            data={data}
            description={`${t('dashboard.expiring_hint')} · ${t('dashboard.window', { count: windowDays }).toLowerCase()}`}
            displayColumnDefOptions={{ 'mrt-row-actions': { header: '', size: 120 } }}
            enableRowActions
            icon={<IconClockExclamation />}
            initialState={{ sorting: [{ id: 'days_left', desc: false }] }}
            onRowClick={openItem}
            renderEmptyRowsFallback={() => <Text className="remnac-empty">{t('dashboard.nothing_expiring')}</Text>}
            renderRowActions={({ row }) => (
                <Button
                    leftSection={<IconCalendarPlus size={14} />}
                    onClick={() => openExtendModal({ kind: row.original.kind, id: row.original.id, title: `${row.original.customer_name} · ${row.original.title}` })}
                    size="compact-sm"
                    variant="default"
                >
                    {t('dashboard.extend')}
                </Button>
            )}
            storageKey="dashboard-expiring"
            title={t('dashboard.expiring')}
        />
    )
}

// TrafficStatsBlock splits live panel users by what's left of their traffic.
function TrafficStatsBlock({ stats, items }: { stats: Dashboard['traffic_stats']; items: TrafficItem[] }) {
    const { t } = useTranslation()
    const parts = [
        { key: 'ok', value: stats.ok, color: 'teal', label: t('dashboard.ts_ok') },
        { key: 'low', value: stats.low, color: 'yellow', label: t('dashboard.ts_low') },
        { key: 'limited', value: stats.limited, color: 'red', label: t('dashboard.ts_limited') },
        { key: 'unlimited', value: stats.unlimited, color: 'blue', label: t('dashboard.ts_unlimited') }
    ]
    const total = parts.reduce((s, p) => s + p.value, 0)
    const next = items
        .map((i) => i.next_traffic_reset_at)
        .filter((d): d is string => !!d)
        .sort()[0]
    return (
        <Block description={t('dashboard.traffic_stats_hint')} icon={<IconChartDonut3 />} style={{ height: '100%' }} title={t('dashboard.traffic_stats')}>
            <Stack align="center" gap="lg">
                <RingProgress
                    label={
                        <Stack align="center" gap={0}>
                            <Text className="num" fw={650} fz={28} lh={1.1} style={{ letterSpacing: '-0.03em' }}>
                                {total}
                            </Text>
                            <Text c="dimmed" size="xs">
                                {t('dashboard.ts_users')}
                            </Text>
                        </Stack>
                    }
                    sections={total ? parts.filter((p) => p.value).map((p) => ({ value: (p.value * 100) / total, color: cssColor(p.color), tooltip: `${p.label}: ${p.value}` })) : []}
                    size={168}
                    thickness={14}
                />
                <Stack gap={8} w="100%">
                    {parts.map((p) => (
                        <Group gap="xs" justify="space-between" key={p.key} wrap="nowrap">
                            <Group gap={8} wrap="nowrap">
                                <Dot color={p.color} size={8} />
                                <Text c="dimmed" size="sm">
                                    {p.label}
                                </Text>
                            </Group>
                            <Text className="num" fw={600} size="sm">
                                {p.value}
                            </Text>
                        </Group>
                    ))}
                </Stack>
                {next && (
                    <Group justify="space-between" w="100%">
                        <Text c="dimmed" size="sm">
                            {t('dashboard.ts_next_reset')}
                        </Text>
                        <Tooltip label={fmtDate(next)}>
                            <Text c="yellow" fw={500} size="sm">
                                {dayjs(next).fromNow()}
                            </Text>
                        </Tooltip>
                    </Group>
                )}
            </Stack>
        </Block>
    )
}

function TrafficLowTable({ data }: { data: TrafficItem[] }) {
    const { t } = useTranslation()
    const columns = useMemo<MRT_ColumnDef<TrafficItem>[]>(
        () => [
            {
                accessorKey: 'title',
                header: t('dashboard.col_subscription'),
                size: 220,
                Cell: ({ row }) => (
                    <TitleCell
                        color={trafficColor(row.original.used_pct)}
                        customerId={row.original.customer_id}
                        customerName={row.original.customer_name}
                        kind={row.original.kind}
                        title={row.original.title}
                    />
                )
            },
            {
                accessorKey: 'used_pct',
                header: t('dashboard.col_traffic'),
                size: 190,
                sortDescFirst: true,
                Cell: ({ row }) => <TrafficMini user={row.original} w={170} withReset={false} />
            },
            {
                id: 'reset',
                header: t('dashboard.col_reset'),
                size: 150,
                accessorFn: (r) => (r.next_traffic_reset_at ? new Date(r.next_traffic_reset_at) : undefined),
                sortingFn: 'datetime',
                sortUndefined: 'last',
                Cell: ({ row }) => (
                    <Tooltip label={trafficHint(row.original)}>
                        <Stack gap={1}>
                            <Text className="num" fw={500} size="sm">
                                {row.original.next_traffic_reset_at ? fmtDate(row.original.next_traffic_reset_at) : '∞'}
                            </Text>
                            <Text c={trafficResetColor(row.original)} size="xs">
                                {trafficResetText(row.original)}
                            </Text>
                        </Stack>
                    </Tooltip>
                )
            },
            {
                accessorKey: 'status',
                header: t('dashboard.col_status'),
                size: 130,
                Cell: ({ row }) => <StatusPill status={row.original.status} />
            }
        ],
        [t]
    )
    return (
        <DataTableCard
            actions={<Badge color={data.length ? 'yellow' : 'teal'} size="lg">{data.length}</Badge>}
            columns={columns}
            compact
            data={data}
            description={t('dashboard.traffic_low_hint')}
            icon={<IconGauge />}
            initialState={{ sorting: [{ id: 'used_pct', desc: true }] }}
            onRowClick={openItem}
            renderEmptyRowsFallback={() => <Text className="remnac-empty">{t('dashboard.nothing_traffic_low')}</Text>}
            storageKey="dashboard-traffic-low"
            title={t('dashboard.traffic_low')}
        />
    )
}
