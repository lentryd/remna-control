import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { ActionIcon, Badge, Button, Group, Menu, Tabs, Text, Tooltip } from '@mantine/core'
import { IconArrowBackUp, IconArrowUpRight, IconBuildingSkyscraper, IconDots, IconPencil, IconPlus, IconReceipt, IconTrash } from '@tabler/icons-react'
import { useMemo } from 'react'

import { api } from '@/api/client'
import { useExpenses, useInvalidateAll, useProviders } from '@/api/hooks'
import type { Expense, ProviderTotals } from '@/api/types'
import { fmtCurrency, fmtDate, fmtMoney, fmtNum, baseCurrency } from '@/components/format'
import { notifyError } from '@/components/notify'
import { Money, PageHeader } from '@/components/ui'
import { StatStrip } from '@shared/ui/stat-strip'
import { openExpenseForm } from '@/modals/ExpenseModal'
import { confirmDanger } from '@/modals/open'
import { ProviderLabel } from '@shared/ui/infra/provider'
import { Page } from '@shared/ui/page'
import { DataTableCard } from '@shared/ui/table'
import { useTranslation } from 'react-i18next'

export function ExpensesPage() {
    const { t } = useTranslation()
    const expenses = useExpenses()
    const providers = useProviders()
    const invalidate = useInvalidateAll()

    const totals = useMemo(() => {
        let gross = 0
        let refunded = 0
        for (const e of expenses.data ?? []) {
            if (e.rub_amount >= 0) gross += e.rub_amount
            else refunded -= e.rub_amount
        }
        return { gross, refunded, net: gross - refunded }
    }, [expenses.data])

    const columns = useMemo<MRT_ColumnDef<Expense>[]>(
        () => [
            { id: 'date', header: t('customer.col_date'), sortingFn: 'datetime', accessorFn: (r) => new Date(r.date), enableColumnFilter: false, Cell: ({ row }) => <Text className="num" size="sm">{fmtDate(row.original.date)}</Text> },
            {
                accessorKey: 'provider',
                header: t('expense_items.provider'),
                filterVariant: 'multi-select',
                size: 200,
                Cell: ({ row }) => <ProviderLabel name={row.original.provider} uuid={row.original.provider_uuid} />
            },
            { accessorKey: 'item_name', header: t('expense_items.col_item'), filterVariant: 'multi-select' },
            {
                accessorKey: 'kind',
                header: t('backup.col_kind'),
                filterVariant: 'select',
                size: 170,
                mantineTableBodyCellProps: { align: 'center' },
                mantineFilterSelectProps: { data: [{ value: 'charge', label: t('expenses.charge') }, { value: 'refund', label: t('expenses.refund') }] },
                Cell: ({ row }) =>
                    row.original.kind === 'refund' ? (
                        <Badge color="teal" leftSection={<IconArrowBackUp size={13} />} size="md">
                            {t('expenses.refund_badge')}
                        </Badge>
                    ) : (
                        <Badge color="orange" leftSection={<IconArrowUpRight size={13} />} size="md">
                            {t('expenses.charge_badge')}
                        </Badge>
                    )
            },
            {
                id: 'orig',
                header: t('expenses.col_orig'),
                accessorFn: (r) => r.orig_amount,
                Cell: ({ row }) => {
                    const e = row.original
                    return (
                        <Tooltip
                            label={[
                                t('expenses.rate', { rate: fmtNum(e.fx_rate, 4) }),
                                e.fee_percent ? t('expenses.fee', { pct: e.fee_percent }) : '',
                                e.share_percent !== 100 ? t('expense_items.share_badge', { pct: e.share_percent }) : ''
                            ]
                                .filter(Boolean)
                                .join(' · ')}
                        >
                            <Text className="num" size="sm">
                                {fmtCurrency(e.orig_amount, e.orig_currency)}
                            </Text>
                        </Tooltip>
                    )
                }
            },
            { accessorKey: 'rub_amount', header: t('expenses.col_base', { currency: baseCurrency() }), enableColumnFilter: false, Cell: ({ cell }) => <Money value={-cell.getValue<number>()} signed digits={2} /> },
            {
                id: 'extra',
                header: t('expenses.col_note'),
                accessorFn: (r) => r.note,
                Cell: ({ row }) => {
                    const e = row.original
                    return (
                        <Group gap={6} wrap="nowrap">
                            {e.refunded_total > 0 && (
                                <Badge size="xs" color="teal" variant="soft">
                                    {t('expenses.refunded', { amount: fmtMoney(e.refunded_total) })}
                                </Badge>
                            )}
                            {e.calc_rub_amount !== null && e.calc_rub_amount !== e.rub_amount && (
                                <Badge size="xs" color="yellow" variant="soft">
                                    {t('expenses.calculated', { amount: fmtMoney(e.calc_rub_amount, 2) })}
                                </Badge>
                            )}
                            <Text size="sm" truncate maw={280}>
                                {e.note}
                            </Text>
                        </Group>
                    )
                }
            }
        ],
        [t]
    )

    const providerColumns = useMemo<MRT_ColumnDef<ProviderTotals>[]>(
        () => [
            {
                accessorKey: 'provider',
                header: t('expense_items.provider'),
                size: 240,
                Cell: ({ row }) => <ProviderLabel name={row.original.provider} uuid={row.original.provider_uuid} />
            },
            {
                accessorKey: 'gross',
                header: t('expenses.col_gross'),
                Cell: ({ cell }) => <Text className="num" size="sm">{fmtMoney(cell.getValue<number>(), 2)}</Text>
            },
            {
                accessorKey: 'refunded',
                header: t('expenses.col_refunded'),
                Cell: ({ cell }) =>
                    cell.getValue<number>() ? (
                        <Text c="teal" className="num" size="sm">
                            {fmtMoney(cell.getValue<number>(), 2)}
                        </Text>
                    ) : (
                        <Text c="dimmed">–</Text>
                    )
            },
            {
                accessorKey: 'net',
                header: t('expenses.col_net'),
                Cell: ({ cell }) => (
                    <Text className="num" fw={700} size="sm">
                        {fmtMoney(cell.getValue<number>(), 2)}
                    </Text>
                )
            },
            { accessorKey: 'count', header: t('expenses.col_count'), mantineTableBodyCellProps: { align: 'center' } },
            {
                id: 'last',
                header: t('expenses.col_last'),
                sortingFn: 'datetime',
                accessorFn: (r) => new Date(r.last),
                Cell: ({ row }) => <Text className="num" size="sm">{fmtDate(row.original.last)}</Text>
            }
        ],
        [t]
    )

    const remove = (e: Expense) =>
        confirmDanger(t('expenses.delete'), `${e.provider} · ${fmtMoney(e.rub_amount, 2)}`, async () => {
            try {
                await api.del(`expenses/${e.id}`)
                await invalidate()
            } catch (err) {
                notifyError(err)
            }
        })

    return (
        <Page title={t('menu.expenses')}>
            <PageHeader
                icon={<IconReceipt size={24} />}
                title={t('menu.expenses')}
                description={t('expenses.description', { currency: baseCurrency() })}
                actions={
                    <Button leftSection={<IconPlus size={16} />} onClick={() => openExpenseForm({})} variant="filled">
                        {t('expenses.expense')}
                    </Button>
                }
            />
            <StatStrip
                items={[
                    { label: t('expenses.total_gross'), value: fmtMoney(totals.gross, 2), hint: t('expenses.before_refunds') },
                    { label: t('expenses.col_refunded'), value: fmtMoney(totals.refunded, 2), color: totals.refunded ? 'teal' : undefined },
                    { label: t('expenses.total_net'), value: fmtMoney(totals.net, 2) }
                ]}
                mb="lg"
            />
            <Tabs defaultValue="journal">
                <Tabs.List mb="md">
                    <Tabs.Tab value="journal">{t('expenses.journal')}</Tabs.Tab>
                    <Tabs.Tab value="providers">{t('expenses.by_provider')}</Tabs.Tab>
                </Tabs.List>
                <Tabs.Panel value="journal">
                    <DataTableCard
                        storageKey="expenses"
                        icon={<IconReceipt size={24} />}
                        title={t('expenses.journal_title')}
                        columns={columns}
                        data={expenses.data ?? []}
                        state={{ showProgressBars: expenses.isFetching, isLoading: !expenses.data }}
                        onRowClick={(e) => openExpenseForm({ expense: e })}
                        initialState={{ sorting: [{ id: 'date', desc: true }] }}
                        enableRowActions
                        positionActionsColumn="last"
                        renderRowActions={({ row }) => (
                            <Menu position="bottom-end" withinPortal>
                                <Menu.Target>
                                    <ActionIcon variant="subtle" color="gray">
                                        <IconDots size={18} />
                                    </ActionIcon>
                                </Menu.Target>
                                <Menu.Dropdown>
                                    <Menu.Item leftSection={<IconPencil size={16} />} onClick={() => openExpenseForm({ expense: row.original })}>
                                        {t('common.edit')}
                                    </Menu.Item>
                                    {row.original.kind === 'charge' && (
                                        <Menu.Item leftSection={<IconArrowBackUp size={16} />} onClick={() => openExpenseForm({ refundOf: row.original })}>
                                            {t('expenses.make_refund')}
                                        </Menu.Item>
                                    )}
                                    <Menu.Divider />
                                    <Menu.Item color="red" leftSection={<IconTrash size={16} />} onClick={() => remove(row.original)}>
                                        {t('common.delete')}
                                    </Menu.Item>
                                </Menu.Dropdown>
                            </Menu>
                        )}
                    />
                </Tabs.Panel>
                <Tabs.Panel value="providers">
                    <DataTableCard
                        storageKey="providers"
                        icon={<IconBuildingSkyscraper size={24} />}
                        title={t('expenses.by_provider')}
                        description={t('expenses.by_provider_hint')}
                        compact
                        columns={providerColumns}
                        data={providers.data ?? []}
                        state={{ isLoading: !providers.data }}
                        initialState={{ sorting: [{ id: 'gross', desc: true }] }}
                    />
                </Tabs.Panel>
            </Tabs>
        </Page>
    )
}
