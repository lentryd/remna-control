import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Anchor, Badge, Button, Group, SegmentedControl, Stack } from '@mantine/core'
import { useMemo, useState } from 'react'
import { IconHexagon, IconLink } from '@tabler/icons-react'
import { Link } from 'react-router'

import { useSubscriptions } from '@/api/hooks'
import type { AddonItem, RwUser, Subscription } from '@/api/types'
import { Dot, ExpireCell, OnlineCell, StatusBadge, TrafficCell, UsernameCell, statusLabel } from '@/components/badges'
import { daysLeft, isTrafficLow, trafficPct } from '@/components/format'
import { AddonActions, SubscriptionActions } from '@/components/ItemActions'
import { Money } from '@/components/ui'
import { openSubscriptionForm } from '@/modals/SubscriptionModals'
import { openViewAddonModal, openViewSubscriptionModal } from '@/modals/ViewItemModal'
import { StatStrip, type StatStripItem } from '@shared/ui/stat-strip'
import { Page } from '@shared/ui/page'
import { PageHeaderShared } from '@shared/ui/page-header'
import { DataTableCard } from '@shared/ui/table'
import { useTranslation } from 'react-i18next'

interface Row {
    key: string
    kind: 'subscription' | 'addon'
    title: string
    customer_id: number
    customer_name: string
    tariff: string
    price: number
    rw: RwUser | null
    auto: boolean
    sub: Subscription
    addon?: AddonItem
}

export function SubscriptionsPage() {
    const { t } = useTranslation()
    const { data, isFetching } = useSubscriptions()
    const [scope, setScope] = useState('active')

    const rows = useMemo(() => {
        const out: Row[] = []
        for (const s of data ?? []) {
            if (scope === 'active' && s.customer_archived) continue
            out.push({
                key: `s${s.id}`,
                kind: 'subscription',
                title: s.title,
                customer_id: s.customer_id,
                customer_name: s.customer_name,
                tariff: s.tariff_name,
                price: s.price,
                rw: s.rw_user,
                auto: s.auto_extend,
                sub: s
            })
            for (const a of s.addons) {
                out.push({
                    key: `a${a.id}`,
                    kind: 'addon',
                    title: `${a.addon_name} · ${s.title}`,
                    customer_id: s.customer_id,
                    customer_name: s.customer_name,
                    tariff: a.tariff_name,
                    price: a.price,
                    rw: a.rw_user,
                    auto: a.auto_extend,
                    sub: s,
                    addon: a
                })
            }
        }
        if (scope === 'expiring') {
            return out.filter((r) => {
                const d = daysLeft(r.rw?.expire_at)
                return d !== null && d <= 7 && d >= -30 && !r.sub.customer_archived
            })
        }
        if (scope === 'traffic') {
            return out.filter((r) => r.rw && !r.rw.deleted && r.rw.status !== 'DISABLED' && isTrafficLow(r.rw) && !r.sub.customer_archived)
        }
        if (scope === 'unlinked') return out.filter((r) => !r.rw)
        return out
    }, [data, scope])

    const columns = useMemo<MRT_ColumnDef<Row>[]>(
        () => [
            {
                accessorKey: 'title',
                header: t('dashboard.col_subscription'),
                size: 260,
                Cell: ({ row }) => (
                    <UsernameCell
                        user={row.original.rw}
                        title={row.original.title}
                        badge={
                            row.original.kind === 'addon' && (
                                <Badge size="xs" color="grape" variant="soft">
                                    {row.original.addon?.included ? t('subscriptions.addon_included') : t('dashboard.addon_badge')}
                                </Badge>
                            )
                        }
                    />
                )
            },
            {
                accessorKey: 'customer_name',
                header: t('sub.customer'),
                Cell: ({ row }) => (
                    <Anchor component={Link} to={`/customers/${row.original.customer_id}`} size="sm">
                        {row.original.customer_name}
                    </Anchor>
                )
            },
            {
                id: 'status',
                header: t('dashboard.col_status'),
                accessorFn: (r) => (r.rw ? r.rw.status : 'UNLINKED'),
                filterVariant: 'multi-select',
                mantineFilterMultiSelectProps: {
                    data: [
                        ...['ACTIVE', 'EXPIRED', 'LIMITED', 'DISABLED', 'UNLINKED'].map((value) => ({ value, label: statusLabel(value) }))
                    ]
                },
                size: 190,
                mantineTableBodyCellProps: { align: 'center' },
                Cell: ({ row }) => <StatusBadge user={row.original.rw} />
            },
            {
                id: 'expire',
                header: t('sub.paid_until'),
                sortingFn: 'datetime',
                accessorFn: (r) => (r.rw?.expire_at ? new Date(r.rw.expire_at) : undefined),
                sortDescFirst: false,
                sortUndefined: 'last',
                enableColumnFilter: false,
                mantineTableBodyCellProps: { align: 'center' },
                Cell: ({ row }) => <ExpireCell date={row.original.rw?.expire_at} />
            },
            { accessorKey: 'tariff', header: t('tariffs.tariff'), filterVariant: 'multi-select' },
            { accessorKey: 'price', header: t('dashboard.col_price'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} /> },
            {
                id: 'traffic',
                header: t('sub.traffic'),
                // Sorted by the share of the limit used; unlimited users go last.
                accessorFn: (r) => trafficPct(r.rw) ?? -1,
                sortDescFirst: true,
                enableColumnFilter: false,
                size: 260,
                mantineTableBodyCellProps: { align: 'center' },
                Cell: ({ row }) => <TrafficCell user={row.original.rw} />
            },
            {
                id: 'online',
                header: t('subscriptions.col_online'),
                size: 190,
                sortingFn: 'datetime',
                accessorFn: (r) => (r.rw?.online_at ? new Date(r.rw.online_at) : undefined),
                sortUndefined: 'last',
                enableColumnFilter: false,
                Cell: ({ row }) => <OnlineCell user={row.original.rw} />
            },
            { id: 'username', header: 'Username', accessorFn: (r) => r.rw?.username ?? '' },
            {
                accessorKey: 'auto',
                header: t('subscriptions.col_auto'),
                enableColumnFilter: false,
                Cell: ({ cell }) => (cell.getValue<boolean>() ? t('common.yes') : t('common.no'))
            }
        ],
        [t]
    )

    const stats = useMemo(() => {
        const all = (data ?? []).filter((s) => !s.customer_archived).flatMap((s) => [s.rw_user, ...s.addons.map((a) => a.rw_user)])
        const count = (f: (u: (typeof all)[number]) => boolean) => all.filter(f).length
        return { all, count }
    }, [data])

    const tile = (key: string, label: string, value: number, color: string, scopeKey?: string): StatStripItem => ({
        key,
        label: (
            <Group component="span" gap={6} wrap="nowrap">
                <Dot color={color} />
                {label}
            </Group>
        ),
        value: data ? value : '—',
        onClick: scopeKey ? () => setScope(scopeKey) : undefined,
        active: !!scopeKey && scope === scopeKey
    })
    const tiles: StatStripItem[] = [
        tile('total', t('subscriptions.total'), stats.all.length, 'blue', 'active'),
        tile('active', t('subscriptions.active'), stats.count((u) => u?.status === 'ACTIVE'), 'teal'),
        tile(
            'expiring',
            t('subscriptions.expiring_7'),
            stats.count((u) => {
                const d = daysLeft(u?.expire_at)
                return u?.status === 'ACTIVE' && d !== null && d >= 0 && d <= 7
            }),
            'orange',
            'expiring'
        ),
        tile('traffic', t('dashboard.traffic_low'), stats.count((u) => !!u && (u.status === 'ACTIVE' || u.status === 'LIMITED') && isTrafficLow(u)), 'yellow', 'traffic'),
        tile('expired', t('subscriptions.expired'), stats.count((u) => u?.status === 'EXPIRED'), 'red'),
        tile('unlinked', t('subscriptions.unlinked'), stats.count((u) => !u), 'gray', 'unlinked')
    ]

    return (
        <Page title={t('menu.subscriptions')}>
            <PageHeaderShared
                actions={
                    <Button leftSection={<IconLink size={16} />} onClick={() => openSubscriptionForm({})} variant="filled">
                        {t('sub.link')}
                    </Button>
                }
                title={t('menu.subscriptions')}
                description={t('subscriptions.description')}
            />
            <Stack gap="lg">
                <StatStrip items={tiles} />

                <DataTableCard
                    storageKey="subscriptions"
                    icon={<IconHexagon size={24} />}
                    title={t('subscriptions.table_title')}
                    actions={
                        <>
                            <SegmentedControl
                                size="xs"
                                value={scope}
                                onChange={setScope}
                                data={[
                                    { value: 'active', label: t('subscriptions.scope_active') },
                                    { value: 'expiring', label: t('subscriptions.scope_expiring') },
                                    { value: 'traffic', label: t('subscriptions.scope_traffic') },
                                    { value: 'unlinked', label: t('subscriptions.unlinked') },
                                    { value: 'all', label: t('common.all') }
                                ]}
                            />
                        </>
                    }
                    columns={columns}
                    data={rows}
                    getRowId={(r) => r.key}
                    state={{ showProgressBars: isFetching, isLoading: !data }}
                    initialState={{ sorting: [{ id: 'expire', desc: false }], columnVisibility: { username: false, auto: false } }}
                    enableRowActions
                    renderRowActions={({ row }) =>
                        row.original.kind === 'addon' ? (
                            <AddonActions addon={row.original.addon!} subTitle={row.original.sub.title} />
                        ) : (
                            <SubscriptionActions sub={row.original.sub} />
                        )
                    }
                    onRowClick={(r) => (r.kind === 'addon' ? openViewAddonModal(r.addon!.id, r.sub.id) : openViewSubscriptionModal(r.sub))}
                />
            </Stack>
        </Page>
    )
}
