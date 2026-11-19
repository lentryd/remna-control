import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import {
    Alert,
    Anchor,
    Badge,
    Button,
    Card,
    Group,
    Menu,
    SimpleGrid,
    Stack,
    Tabs,
    Text
} from '@mantine/core'
import { IconCalendarPlus, IconChevronDown, IconCreditCard, IconLink, IconPencil, IconPlus, IconScale, IconTrash, IconUserPlus, IconWallet } from '@tabler/icons-react'
import { Link, useNavigate, useParams } from 'react-router'

import { api } from '@/api/client'
import { useCustomer, useInvalidateAll } from '@/api/hooks'
import type { CustomerDetail, Extension, LedgerEntry } from '@/api/types'
import { durationLabel, fmtDate, fmtDateTime, fmtMoney } from '@/components/format'
import { notifyError, errorText } from '@/components/notify'
import { paymentColumns, paymentTableProps } from '@/components/PaymentTable'
import { SubscriptionCard } from '@/components/SubscriptionCard'
import { BackLink, Money, PageHeader } from '@/components/ui'
import { openAdjustModal, openCustomerForm } from '@/modals/CustomerModals'
import { confirmDanger } from '@/modals/open'
import { openPaymentModal } from '@/modals/PaymentModal'
import { openProvisionModal, openSubscriptionForm } from '@/modals/SubscriptionModals'
import { LoadingScreen } from '@shared/ui/loading-screen'
import { Page } from '@shared/ui/page'
import { StatStrip } from '@shared/ui/stat-strip'
import { DataTableCard } from '@shared/ui/table'
import i18n from '@/app/i18n/i18n'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

const ledgerColor: Record<string, string> = { payment: 'teal', charge: 'orange', adjustment: 'blue', refund: 'grape' }
const ledgerLabel = (type: string) =>
    ['payment', 'charge', 'adjustment', 'refund', 'referral'].includes(type) ? i18n.t(`customer.ledger.${type as 'payment'}`) : type
const extKind = (kind: string) =>
    ['extend', 'connect', 'tariff_change'].includes(kind) ? i18n.t(`customer.ext.${kind as 'extend'}`) : kind

const mono = (v: string) => (
    <Text className="num" size="sm">
        {v}
    </Text>
)

// Columns are built per render language (see CustomerPage).
const ledgerColumns = (): MRT_ColumnDef<LedgerEntry>[] => [
    { id: 'date', header: i18n.t('customer.col_date'), sortingFn: 'datetime', accessorFn: (r) => new Date(r.date), Cell: ({ row }) => mono(fmtDateTime(row.original.date)) },
    {
        accessorKey: 'type',
        header: i18n.t('customer.col_operation'),
        Cell: ({ row }) => (
            <Badge color={ledgerColor[row.original.type]} size="md">
                {ledgerLabel(row.original.type)}
            </Badge>
        )
    },
    {
        accessorKey: 'amount',
        header: i18n.t('customer.col_amount'),
        Cell: ({ cell }) => (
            <Text c={cell.getValue<number>() > 0 ? 'teal' : 'red'} className="num" fw={600} size="sm">
                {cell.getValue<number>() > 0 ? '+' : ''}
                {fmtMoney(cell.getValue<number>(), 2)}
            </Text>
        )
    },
    { accessorKey: 'note', header: i18n.t('customer.col_note'), size: 320 }
]

const extColumns = (): MRT_ColumnDef<Extension>[] => [
    {
        accessorKey: 'created_at',
        header: i18n.t('customer.col_when'),
        sortingFn: 'datetime',
        accessorFn: (r) => new Date(r.created_at),
        Cell: ({ row }) => mono(fmtDateTime(row.original.created_at))
    },
    {
        accessorKey: 'kind',
        header: i18n.t('customer.col_action'),
        Cell: ({ row }) => (
            <Group gap={6} wrap="nowrap">
                <Badge color={row.original.status === 'failed' ? 'red' : 'teal'} variant="soft">
                    {extKind(row.original.kind)}
                </Badge>
                {row.original.subscription_addon_id && (
                    <Badge color="grape" size="xs" variant="soft">
                        {i18n.t('dashboard.addon_badge')}
                    </Badge>
                )}
                {row.original.status === 'failed' && (
                    <Text c="red" size="xs" truncate="end" title={row.original.error}>
                        {row.original.error}
                    </Text>
                )}
            </Group>
        )
    },
    { id: 'term', header: i18n.t('dashboard.col_term'), accessorFn: (r) => durationLabel(r.months, r.days) },
    { accessorKey: 'amount', header: i18n.t('customer.col_amount'), Cell: ({ cell }) => mono(fmtMoney(cell.getValue<number>(), 2)) },
    {
        id: 'period',
        header: i18n.t('expense_items.period'),
        size: 220,
        accessorFn: (r) => r.to_at,
        Cell: ({ row }) => (row.original.from_at ? mono(`${fmtDate(row.original.from_at)} → ${fmtDate(row.original.to_at)}`) : '–')
    },
    { accessorKey: 'actor', header: i18n.t('customer.col_actor') }
]

export function CustomerPage() {
    const { t } = useTranslation()
    const id = Number(useParams().id)
    const { data, isPending, error } = useCustomer(id)
    const navigate = useNavigate()
    const invalidate = useInvalidateAll()
    const cols = useMemo(() => ({ payments: paymentColumns(false), ledger: ledgerColumns(), ext: extColumns() }), [t])

    if (isPending) {
        return <LoadingScreen height="60vh" />
    }
    if (error || !data) return <Alert color="red">{error ? errorText(error) : t('errors.customer.not_found')}</Alert>
    const c: CustomerDetail = data

    const remove = () =>
        confirmDanger(t('customer.delete'), t('customer.delete_hint'), async () => {
            try {
                await api.del(`customers/${c.id}`)
                await invalidate()
                navigate('/customers')
            } catch (e) {
                notifyError(e)
            }
        })

    return (
        <Page title={c.name}>
            <PageHeader
                eyebrow={<BackLink to="/customers">{t('menu.customers')}</BackLink>}
                title={c.name}
                badges={
                    c.archived && (
                        <Badge color="gray" size="lg">
                            {t('customer.archived')}
                        </Badge>
                    )
                }
                description={
                    <Group gap={6} component="span">
                        {c.referrer_id ? (
                            <span>
                                {t('customer.referred_by')}{' '}
                                <Anchor component={Link} to={`/customers/${c.referrer_id}`} size="sm">
                                    {c.referrer_name}
                                </Anchor>
                            </span>
                        ) : (
                            <span>{t('customer.came_alone')}</span>
                        )}
                        {c.contact && <span>· {c.contact}</span>}
                        {c.telegram_id && <span>· TG {c.telegram_id}</span>}
                    </Group>
                }
                actions={
                    <>
                        <Button variant="filled" leftSection={<IconCreditCard size={16} />} onClick={() => openPaymentModal({ customerId: c.id, name: c.name })}>
                            {t('customer.record_payment')}
                        </Button>
                        <Menu position="bottom-end">
                            <Menu.Target>
                                <Button rightSection={<IconChevronDown size={14} />}>
                                    {t('customer.more')}
                                </Button>
                            </Menu.Target>
                            <Menu.Dropdown>
                                <Menu.Item leftSection={<IconUserPlus size={16} />} onClick={() => openProvisionModal(c)}>
                                    {t('customer.create_subscription')}
                                </Menu.Item>
                                <Menu.Item leftSection={<IconLink size={16} />} onClick={() => openSubscriptionForm({ customerId: c.id })}>
                                    {t('sub.link_title')}
                                </Menu.Item>
                                <Menu.Item leftSection={<IconScale size={16} />} onClick={() => openAdjustModal(c)}>
                                    {t('customer.adjust_balance')}
                                </Menu.Item>
                                <Menu.Item leftSection={<IconPencil size={16} />} onClick={() => openCustomerForm(c)}>
                                    {t('common.edit')}
                                </Menu.Item>
                                <Menu.Divider />
                                <Menu.Item color="red" leftSection={<IconTrash size={16} />} onClick={remove}>
                                    {t('common.delete')}
                                </Menu.Item>
                            </Menu.Dropdown>
                        </Menu>
                    </>
                }
            />

            <StatStrip
                items={[
                    {
                        label: t('dashboard.col_balance'),
                        value: <Money value={c.balance} signed digits={2} />,
                        color: c.balance < 0 ? 'red' : undefined
                    },
                    { label: t('customer.balance_own'), value: <Money value={c.balance_own} signed digits={2} />, color: c.balance_own < 0 ? 'red' : undefined },
                    { label: t('customer.balance_referral'), value: <Money value={c.balance_referral} signed digits={2} />, color: c.balance_referral > 0 ? 'grape' : undefined },
                    { label: t('sub.per_month'), value: <Money value={c.monthly} /> },
                    { label: t('customer.total_paid'), value: <Money value={c.total_paid} />, hint: c.last_payment_at ? t('customer.last_payment', { date: fmtDate(c.last_payment_at) }) : undefined },
                    { label: t('customer.referred_count'), value: c.referrals_count }
                ]}
                mb="lg"
            />

            {c.notes && (
                <Card mb="lg" padding="md">
                    <Text c="dimmed" fw={500} mb={4} size="xs">
                        {t('customer.notes')}
                    </Text>
                    <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
                        {c.notes}
                    </Text>
                </Card>
            )}

            <Tabs defaultValue="subs" keepMounted={false}>
                <Tabs.List mb="md">
                    <Tabs.Tab value="subs">{t('customer.tab_subs', { count: c.subscriptions.length })}</Tabs.Tab>
                    <Tabs.Tab value="payments">{t('customer.tab_payments', { count: c.payments?.length ?? 0 })}</Tabs.Tab>
                    <Tabs.Tab value="ledger">{t('dashboard.col_balance')}</Tabs.Tab>
                    <Tabs.Tab value="refs">{t('customer.tab_refs', { count: c.referrals?.length ?? 0 })}</Tabs.Tab>
                    <Tabs.Tab value="ext">{t('customer.tab_ext')}</Tabs.Tab>
                </Tabs.List>

                <Tabs.Panel value="subs">
                    <Stack>
                        {c.subscriptions.map((s) => (
                            <SubscriptionCard key={s.id} sub={s} />
                        ))}
                        {c.subscriptions.length === 0 && (
                            <Text c="dimmed">{t('customer.no_subs')}</Text>
                        )}
                        <Group>
                            <Button leftSection={<IconPlus size={16} />} onClick={() => openProvisionModal(c)} variant="light">
                                {t('sub.create_in_panel')}
                            </Button>
                            <Button leftSection={<IconLink size={16} />} onClick={() => openSubscriptionForm({ customerId: c.id })}>
                                {t('customer.link_existing')}
                            </Button>
                        </Group>
                    </Stack>
                </Tabs.Panel>

                <Tabs.Panel value="payments">
                    <DataTableCard
                        compact
                        storageKey="customer-payments"
                        icon={<IconCreditCard size={24} />}
                        title={t('menu.payments')}
                        {...paymentTableProps}
                        columns={cols.payments}
                        data={(c.payments ?? []).map((p) => ({ ...p, customer_name: c.name }))}
                    />
                </Tabs.Panel>

                <Tabs.Panel value="ledger">
                    <DataTableCard
                        compact
                        storageKey="customer-ledger"
                        icon={<IconWallet size={24} />}
                        title={t('customer.ledger_title')}
                        columns={cols.ledger}
                        data={c.ledger ?? []}
                        initialState={{ sorting: [{ id: 'date', desc: true }] }}
                    />
                </Tabs.Panel>

                <Tabs.Panel value="refs">
                    <SimpleGrid cols={{ base: 1, md: 2 }}>
                        <Card>
                            <Text fw={600} mb="sm">
                                {t('customer.referred')}
                            </Text>
                            <Stack gap={6}>
                                {(c.referrals ?? []).map((r) => (
                                    <Group key={r.id} justify="space-between">
                                        <Anchor component={Link} to={`/customers/${r.id}`} size="sm" c={r.archived ? 'dimmed' : undefined}>
                                            {r.name}
                                        </Anchor>
                                        <Text size="xs" c="dimmed">
                                            {r.monthly ? t('customer.ref_monthly', { amount: fmtMoney(r.monthly) }) : t('customer.ref_inactive')} · {t('customer.ref_paid', { amount: fmtMoney(r.total_paid) })}
                                        </Text>
                                    </Group>
                                ))}
                                {!c.referrals?.length && (
                                    <Text c="dimmed" size="sm">
                                        {t('customer.nobody')}
                                    </Text>
                                )}
                            </Stack>
                        </Card>
                        <Card>
                            <Text fw={600} mb="sm">
                                {t('customer.accruals')}
                            </Text>
                            <Stack gap={6}>
                                {(c.accruals ?? []).map((a) => (
                                    <Group key={a.id} justify="space-between" wrap="nowrap">
                                        <Text size="sm">
                                            {fmtDate(a.date)} · {a.referrer_id === c.id ? t('customer.accrual_from', { name: a.referee_name }) : t('customer.accrual_for', { name: a.referrer_name })}
                                        </Text>
                                        <Text size="sm" c={a.referrer_id === c.id ? 'teal' : 'dimmed'}>
                                            {fmtMoney(a.amount, 2)} ({a.percent}%)
                                        </Text>
                                    </Group>
                                ))}
                                {!c.accruals?.length && (
                                    <Text c="dimmed" size="sm">
                                        {t('common.none')}
                                    </Text>
                                )}
                            </Stack>
                        </Card>
                    </SimpleGrid>
                </Tabs.Panel>

                <Tabs.Panel value="ext">
                    <DataTableCard
                        compact
                        storageKey="customer-extensions"
                        icon={<IconCalendarPlus size={24} />}
                        title={t('customer.tab_ext')}
                        description={t('customer.ext_hint')}
                        columns={cols.ext}
                        data={c.extensions ?? []}
                        initialState={{ sorting: [{ id: 'created_at', desc: true }] }}
                    />
                </Tabs.Panel>
            </Tabs>
        </Page>
    )
}
