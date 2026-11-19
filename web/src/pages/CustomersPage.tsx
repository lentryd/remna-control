import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { ActionIcon, Badge, Button, Group, SegmentedControl, Text, Tooltip } from '@mantine/core'
import { useMemo, useState } from 'react'
import { IconCreditCard, IconPencil, IconUserPlus, IconUsers } from '@tabler/icons-react'
import { useNavigate } from 'react-router'

import { useCustomers } from '@/api/hooks'
import type { Customer } from '@/api/types'
import { ExpireCell } from '@/components/badges'
import { daysLeft, fmtDate, fmtMoney } from '@/components/format'
import { StatStrip } from '@shared/ui/stat-strip'
import { Money, PageHeader } from '@/components/ui'
import { Page } from '@shared/ui/page'
import { DataTableCard } from '@shared/ui/table'
import { openCustomerForm } from '@/modals/CustomerModals'
import { openPaymentModal } from '@/modals/PaymentModal'
import { useTranslation } from 'react-i18next'

const expiringSoon = (c: Customer) => {
    const d = daysLeft(c.nearest_expire_at)
    return d !== null && d >= 0 && d <= 7
}

export function CustomersPage() {
    const { t } = useTranslation()
    const { data, isFetching } = useCustomers()
    const navigate = useNavigate()
    const active = useMemo(() => (data ?? []).filter((c) => !c.archived), [data])
    const [scope, setScope] = useState('active')
    const rows = useMemo(
        () =>
            (data ?? []).filter((c) => {
                if (scope === 'all') return true
                if (scope === 'archived') return c.archived
                if (scope === 'expiring') return !c.archived && expiringSoon(c)
                if (scope === 'debtors') return !c.archived && c.balance < 0
                return !c.archived
            }),
        [data, scope]
    )
    // a stat tile filters the table, a second click goes back to all active
    const tileScope = (key: string) => ({ onClick: () => setScope(scope === key ? 'active' : key), active: scope === key })

    const columns = useMemo<MRT_ColumnDef<Customer>[]>(
        () => [
            {
                accessorKey: 'name',
                header: t('sub.customer'),
                Cell: ({ row }) => (
                    <Group gap={6} wrap="nowrap" pl={10}>
                        <Text fw={500} size="sm">
                            {row.original.name}
                        </Text>
                        {row.original.archived && (
                            <Badge size="xs" color="gray" variant="soft">
                                {t('customer.archived')}
                            </Badge>
                        )}
                    </Group>
                )
            },
            { accessorKey: 'referrer_name', header: t('customers.referrer'), Cell: ({ cell }) => cell.getValue<string>() || <Text c="dimmed" size="sm">—</Text> },
            {
                accessorKey: 'nearest_expire_at',
                header: t('customers.col_next_expiry'),
                sortingFn: 'datetime',
                accessorFn: (r) => (r.nearest_expire_at ? new Date(r.nearest_expire_at) : undefined),
                sortDescFirst: false,
                sortUndefined: 'last',
                enableColumnFilter: false,
                mantineTableBodyCellProps: { align: 'center' },
                Cell: ({ row }) => <ExpireCell date={row.original.nearest_expire_at} />
            },
            { accessorKey: 'monthly', header: t('sub.per_month'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} /> },
            { accessorKey: 'balance', header: t('dashboard.col_balance'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} signed digits={2} /> },
            { accessorKey: 'balance_own', header: t('customer.balance_own'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} signed digits={2} /> },
            {
                accessorKey: 'balance_referral',
                header: t('customer.balance_referral'),
                enableColumnFilter: false,
                Cell: ({ cell }) => (cell.getValue<number>() ? <Text c="grape" size="sm"><Money value={cell.getValue<number>()} signed digits={2} /></Text> : <Text c="dimmed" size="sm">—</Text>)
            },
            {
                accessorKey: 'subscriptions_count',
                header: t('menu.subscriptions'),
                enableColumnFilter: false,
                Cell: ({ row }) => (
                    <Text size="sm">
                        {row.original.subscriptions_count}
                        {row.original.addons_count > 0 && <Text span c="grape" size="sm">{` ${t('customers.plus_addons', { count: row.original.addons_count })}`}</Text>}
                    </Text>
                )
            },
            { accessorKey: 'referrals_count', header: t('customers.col_referred'), enableColumnFilter: false },
            { accessorKey: 'total_paid', header: t('customer.total_paid'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} /> },
            {
                accessorKey: 'last_payment_at',
                header: t('customers.col_last_payment'),
                sortingFn: 'datetime',
                accessorFn: (r) => (r.last_payment_at ? new Date(r.last_payment_at) : undefined),
                sortUndefined: 'last',
                enableColumnFilter: false,
                Cell: ({ row }) => <Text size="sm">{fmtDate(row.original.last_payment_at)}</Text>
            },
            { accessorKey: 'contact', header: t('customers.contact') },
            { accessorKey: 'telegram_id', header: t('customers.telegram_id'), Cell: ({ cell }) => cell.getValue<number | null>() ?? '—' }
        ],
        [t]
    )

    return (
        <Page title={t('menu.customers')}>
            <PageHeader
                icon={<IconUsers size={24} />}
                title={t('menu.customers')}
                description={t('customers.description')}
                actions={
                    <Button leftSection={<IconUserPlus size={16} />} onClick={() => openCustomerForm()} variant="filled">
                        {t('customers.new')}
                    </Button>
                }
            />
            <StatStrip
                items={[
                    {
                        label: t('customers.scope_active'),
                        value: data ? active.length : '—',
                        hint: t('customers.stat_total', { count: data?.length ?? 0 }),
                        ...tileScope('active')
                    },
                    { label: t('sub.per_month'), value: data ? fmtMoney(active.reduce((n, c) => n + c.monthly, 0)) : '—' },
                    {
                        label: t('customers.stat_expiring'),
                        value: data ? active.filter(expiringSoon).length : '—',
                        color: 'orange',
                        ...tileScope('expiring')
                    },
                    {
                        label: t('customers.stat_debtors'),
                        value: data ? active.filter((c) => c.balance < 0).length : '—',
                        hint: data ? fmtMoney(active.filter((c) => c.balance < 0).reduce((n, c) => n + c.balance, 0), 2) : undefined,
                        ...tileScope('debtors')
                    }
                ]}
                mb="lg"
            />
            <DataTableCard
                storageKey="customers"
                icon={<IconUsers size={24} />}
                title={t('customers.list')}
                actions={
                    <SegmentedControl
                        size="xs"
                        value={scope}
                        onChange={setScope}
                        data={[
                            { value: 'active', label: t('customers.scope_active') },
                            { value: 'expiring', label: t('customers.scope_expiring') },
                            { value: 'debtors', label: t('customers.stat_debtors') },
                            { value: 'archived', label: t('customers.scope_archived') },
                            { value: 'all', label: t('common.all') }
                        ]}
                    />
                }
                columns={columns}
                data={rows}
                state={{ showProgressBars: isFetching, isLoading: !data }}
                initialState={{ sorting: [{ id: 'nearest_expire_at', desc: false }], columnVisibility: { contact: false, telegram_id: false } }}
                enableRowActions
                renderRowActions={({ row }) => (
                    <Group gap={6} wrap="nowrap">
                        <Tooltip label={t('common.edit')} withArrow>
                            <ActionIcon size="md" variant="default" onClick={() => openCustomerForm(row.original)}>
                                <IconPencil size={14} />
                            </ActionIcon>
                        </Tooltip>
                        <Button
                            size="compact-sm"
                            variant="default"
                            leftSection={<IconCreditCard size={14} />}
                            onClick={() => openPaymentModal({ customerId: row.original.id, name: row.original.name })}
                        >
                            {t('payment.title')}
                        </Button>
                    </Group>
                )}
                displayColumnDefOptions={{ 'mrt-row-actions': { header: '', size: 150 } }}
                onRowClick={(r) => navigate(`/customers/${r.id}`)}
            />
        </Page>
    )
}
