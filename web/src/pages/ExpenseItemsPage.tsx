import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import {
    ActionIcon,
    Alert,
    Badge,
    Button,
    Drawer,
    Group,
    Skeleton,
    NumberInput,
    SegmentedControl,
    Select,
    SimpleGrid,
    Stack,
    Switch,
    Text,
    Textarea,
    TextInput,
    Tooltip
} from '@mantine/core'
import { DateInput, MonthPickerInput } from '@mantine/dates'
import { type UseFormReturnType, useForm } from '@mantine/form'
import dayjs from 'dayjs'
import { useMemo, useState } from 'react'
import {
    PiArchive,
    PiArrowsClockwise,
    PiBuildingsDuotone,
    PiCalendarCheckDuotone,
    PiCalendarDuotone,
    PiChartBar,
    PiChartBarDuotone,
    PiCoinsDuotone,
    PiLock,
    PiNotePencil,
    PiPencilSimple,
    PiPencilSimpleDuotone,
    PiPercent,
    PiPlus,
    PiReceipt,
    PiTextAa,
    PiArrowCounterClockwise,
    PiTrash,
    PiUsersDuotone
} from 'react-icons/pi'
import { TbCloudDataConnection, TbServer2 } from 'react-icons/tb'

import { api } from '@/api/client'
import { useApiMutation, useExpenseItems, useInfra, useInvalidateAll, useMetered, useNodes, useSettings } from '@/api/hooks'
import type { ExpenseItem, GBUnit, MinMode, Tier } from '@/api/types'
import { expiryColor } from '@/components/badges'
import { baseCurrency, currencySymbol, dateLayout, fmtCurrency, fmtDate, fmtMoney, fmtNum } from '@/components/format'
import { notifyError, notifyOk, codedText } from '@/components/notify'
import { PageHeader } from '@/components/ui'
import { confirmDanger, openModal } from '@/modals/open'
import { openExpenseForm } from '@/modals/ExpenseModal'
import { MeteredCard } from '@/components/MeteredCard'
import { FormFooter, FormSection, FormStack } from '@shared/ui/forms/form-section'
import { NodeLabel, NodeSelect } from '@shared/ui/infra/node'
import { SquadBadge, SquadSelect } from '@shared/ui/infra/squad'
import { ProviderInput, ProviderLabel } from '@shared/ui/infra/provider'
import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'
import { Page } from '@shared/ui/page'
import { DataTableCard } from '@shared/ui/table'
import { EntityAdd, EntityCard, EntityFigure, EntityGrid, EntityParam } from '@shared/ui/entity-card'
import { FooterFigure } from '@shared/ui/entity-card/footer-figure'
import { StatStrip } from '@shared/ui/stat-strip'
import { SearchSelect } from '@shared/ui/forms/search-select'
import { CurrencyIcon, CURRENCIES } from '@shared/currencies'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

// usePanelBilling maps a node to the panel's Infra Billing record for it.
function usePanelBilling() {
    const { data } = useInfra()
    return useMemo(() => {
        const nodes = data?.billing_nodes ?? []
        return (nodeUuid?: string | null) => (nodeUuid ? nodes.find((b) => b.nodeUuid === nodeUuid) : undefined)
    }, [data])
}

// dueDate is the item's own next payment date, else the panel's Infra
// Billing date for its node; the column sorts by what it shows.
function dueDate(item: ExpenseItem, billing: ReturnType<typeof usePanelBilling>) {
    return item.next_due_date ?? billing(item.rw_node_uuid)?.nextBillingAt ?? null
}

function DueCell({ item, align = 'center' }: { item: ExpenseItem; align?: 'center' | 'flex-end' }) {
    const { t } = useTranslation()
    const billing = usePanelBilling()
    const panel = billing(item.rw_node_uuid)
    const date = dueDate(item, billing)
    if (!date) return <Text c="dimmed">–</Text>
    const days = dayjs(date).diff(dayjs(), 'day')
    return (
        <Stack align={align} gap={0}>
            <Text className="num" fw={600} size="sm">
                {fmtDate(date)}
            </Text>
            <Group gap={4}>
                <Text c={expiryColor(days)} size="xs">
                    {days < 0 ? t('expense_items.overdue', { count: -days }) : days === 0 ? t('expense_items.today') : t('expense_items.in_days', { count: days })}
                </Text>
                {!item.next_due_date && panel && (
                    <Tooltip label={t('expense_items.panel_date')}>
                        <Badge color="gray" size="xs" variant="soft">
                            {t('dashboard.panel_badge')}
                        </Badge>
                    </Tooltip>
                )}
            </Group>
        </Stack>
    )
}

export function ExpenseItemsPage() {
    const { t } = useTranslation()
    const { data } = useExpenseItems()
    const invalidate = useInvalidateAll()
    const [edit, setEdit] = useState<Partial<ExpenseItem> | null>(null)
    const [metered, setMetered] = useState<ExpenseItem | null>(null)
    const [syncing, setSyncing] = useState(false)

    const syncTraffic = async () => {
        setSyncing(true)
        try {
            await api.post('traffic/sync')
            await invalidate()
            notifyOk(t('expense_items.traffic_synced'))
        } catch (e) {
            notifyError(e)
        } finally {
            setSyncing(false)
        }
    }

    const billing = usePanelBilling()
    const items = useMemo(() => {
        const due = (it: ExpenseItem) => {
            const d = dueDate(it, billing)
            return d ? dayjs(d).valueOf() : Infinity
        }
        return [...(data?.items ?? [])].sort((x, y) => Number(y.active) - Number(x.active) || due(x) - due(y))
    }, [data, billing])
    const active = items.filter((x) => x.active)
    const archived = items.filter((x) => !x.active)
    const next = active.find((x) => dueDate(x, billing))
    const create = () => setEdit({ pricing: 'fixed', active: true, currency: baseCurrency(), share_percent: 100, period: 'month' })

    return (
        <Page title={t('menu.expense_items')}>
            <PageHeader
                icon={<PiBuildingsDuotone size={24} />}
                title={t('menu.expense_items')}
                description={t('expense_items.table_hint')}
                actions={
                    <>
                        <Button leftSection={<PiArrowsClockwise size={16} />} loading={syncing} onClick={syncTraffic}>
                            {t('expense_items.sync_traffic')}
                        </Button>
                        <Button variant="filled" leftSection={<PiPlus size={16} />} onClick={create}>
                            {t('expense_items.item')}
                        </Button>
                    </>
                }
            />
            <StatStrip
                items={[
                    { label: t('expense_items.stat_monthly'), value: fmtMoney(data?.monthly_total, 2), hint: t('expense_items.stat_rate') },
                    { label: t('expense_items.stat_items'), value: active.length, hint: t('expense_items.stat_total', { count: items.length }) },
                    {
                        label: t('expense_items.next_payment'),
                        value: next ? fmtDate(dueDate(next, billing)) : '—',
                        hint: next?.name
                    }
                ]}
                mb="lg"
            />
            <EntityGrid min={300}>
                {!data && [0, 1, 2].map((i) => <Skeleton h={250} key={i} radius="lg" />)}
                {active.map((it) => (
                    <ExpenseItemCard item={it} key={it.id} onEdit={() => setEdit(it)} onMetered={() => setMetered(it)} />
                ))}
                {data && active.length === 0 && <EntityAdd label={t('expense_items.new_item')} onClick={create} />}
            </EntityGrid>
            {archived.length > 0 && (
                <>
                    <Text c="var(--app-text-strong)" fw={600} fz={17} mb="md" mt={40}>
                        {t('expense_items.archive_group')}
                    </Text>
                    <EntityGrid min={300}>
                        {archived.map((it) => (
                            <ExpenseItemCard item={it} key={it.id} onEdit={() => setEdit(it)} onMetered={() => setMetered(it)} />
                        ))}
                    </EntityGrid>
                </>
            )}
            <Drawer
                opened={!!edit}
                onClose={() => setEdit(null)}
                position="right"
                size="lg"
                title={
                    <BaseOverlayHeader
                        IconComponent={edit?.id ? PiPencilSimpleDuotone : PiPlus}
                        subtitle={edit?.id ? edit.name : undefined}
                        title={edit?.id ? t('expense_items.item_title') : t('expense_items.new_item')}
                    />
                }
            >
                {edit && <ItemForm item={edit} onDone={() => setEdit(null)} />}
            </Drawer>
            <Drawer
                opened={!!metered}
                onClose={() => setMetered(null)}
                position="right"
                size="xl"
                title={<BaseOverlayHeader IconComponent={PiChartBarDuotone} subtitle={metered?.name} title={t('expense_items.traffic_cost')} />}
            >
                {metered && <MeteredView item={metered} />}
            </Drawer>
        </Page>
    )
}

// ExpenseItemCard is one recurring payment: its price, where it goes, what
// it costs a month in the base currency and when it is due next.
function ExpenseItemCard({ item: it, onEdit, onMetered }: { item: ExpenseItem; onEdit: () => void; onMetered: () => void }) {
    const { t } = useTranslation()
    const invalidate = useInvalidateAll()
    const metered = it.pricing === 'metered'
    const setArchived = async (archive: boolean) => {
        try {
            await api.post(`expense-items/${it.id}/${archive ? 'archive' : 'restore'}`)
            await invalidate()
        } catch (e) {
            notifyError(e)
        }
    }
    const archive = () => confirmDanger(t('expense_items.archive_confirm', { name: it.name }), t('expense_items.archive_hint'), () => setArchived(true))
    const remove = () =>
        confirmDanger(t('expense_items.delete', { name: it.name }), t('expense_items.delete_hint'), async () => {
            try {
                await api.del(`expense-items/${it.id}`)
                await invalidate()
            } catch (e) {
                notifyError(e)
            }
        })
    const extras = [
        it.currency !== baseCurrency() && it.rate && (
            <Tooltip key="rate" label={t('expense_items.col_rate')}>
                <Badge color="gray">
                    1 {it.currency} = {fmtNum(it.rate, 2)} {currencySymbol()}
                </Badge>
            </Tooltip>
        ),
        it.fee_percent > 0 && (
            <Badge color="orange" key="fee">
                +{it.fee_percent}%
            </Badge>
        ),
        it.share_percent !== 100 && (
            <Badge color="indigo" key="share">
                {t('expense_items.share_badge', { pct: it.share_percent })}
            </Badge>
        )
    ].filter(Boolean)

    return (
        <EntityCard
            badges={metered && <Badge color="brand" leftSection={<TbCloudDataConnection size={12} />}>{t('expense_items.metered_badge')}</Badge>}
            dimmed={!it.active}
            dot={it.active ? 'teal' : 'gray'}
            footer={
                <>
                    <FooterFigure label={metered ? `${t('expense_items.monthly_net')} · ${t('expense_items.forecast_badge')}` : t('expense_items.monthly_net')}>
                        {it.plan_error ? (
                            <Tooltip label={codedText(it.plan_error, it.plan_error_code, it.plan_error_params)} maw={320} multiline>
                                <Text c="red" component="span" inherit>
                                    {t('common.error')}
                                </Text>
                            </Tooltip>
                        ) : (
                            fmtMoney(it.monthly_rub, 2)
                        )}
                    </FooterFigure>
                    {it.active && <DueCell align="flex-end" item={it} />}
                </>
            }
            menu={[
                { label: t('expense_items.pay'), icon: PiReceipt, onClick: () => openExpenseForm({ itemId: it.id, name: it.name }) },
                ...(metered
                    ? [
                          { label: t('expense_items.traffic_forecast'), icon: PiChartBar, onClick: onMetered },
                          { label: t('expense_items.close_period'), icon: PiLock, onClick: () => openClosePeriod(it) }
                      ]
                    : []),
                { label: t('common.edit'), icon: PiPencilSimple, onClick: onEdit },
                it.active
                    ? { label: t('expense_items.archive'), icon: PiArchive, color: 'red', onClick: archive }
                    : { label: t('expense_items.restore'), icon: PiArrowCounterClockwise, onClick: () => setArchived(false) },
                ...(it.active ? [] : [{ label: t('common.delete'), icon: PiTrash, color: 'red', onClick: remove }])
            ]}
            onClick={onEdit}
            params={
                <>
                    <EntityParam icon={PiBuildingsDuotone} label={t('expense_items.provider')}>
                        {it.provider ? <ProviderLabel name={it.provider} size="xs" uuid={it.provider_uuid} /> : <Text c="dimmed" component="span" inherit>—</Text>}
                    </EntityParam>
                    {it.rw_node_uuid && (
                        <EntityParam icon={TbServer2} label={t('expense_items.col_node')}>
                            <Group gap={6} wrap="nowrap">
                                <NodeLabel size="xs" uuid={it.rw_node_uuid} />
                                {it.rw_squad_uuid && <SquadBadge size="xs" uuid={it.rw_squad_uuid} />}
                            </Group>
                        </EntityParam>
                    )}
                </>
            }
            subtitle={!it.active ? t('expense_items.inactive') : it.period === 'year' ? t('expense_items.yearly') : t('expense_items.monthly')}
            title={it.name}
        >
            {metered ? (
                <>
                    <EntityFigure
                        unit={it.tiers?.length ? undefined : t('expense_items.per_gb_unit')}
                        value={it.tiers?.length ? t('expense_items.tiers_count', { count: it.tiers.length }) : fmtCurrency(it.price_per_gb, it.currency)}
                    />
                    <Text c="dimmed" mt={6} size="xs">
                        {it.min_mode === 'free'
                            ? t('expense_items.fee_free', { fee: fmtCurrency(it.min_charge, it.currency), gb: fmtNum(it.free_gb) })
                            : t('expense_items.min_short', { min: fmtCurrency(it.min_charge, it.currency) })}
                    </Text>
                </>
            ) : (
                <EntityFigure unit={it.period === 'year' ? t('expense_items.per_year') : t('expense_items.per_month')} value={fmtCurrency(it.amount, it.currency)} />
            )}
            {extras.length > 0 && (
                <Group gap={6} mt="sm">
                    {extras}
                </Group>
            )}
        </EntityCard>
    )
}

function openClosePeriod(item: ExpenseItem) {
    openModal({ icon: PiCalendarCheckDuotone, color: 'orange', title: i18n.t('expense_items.close_period'), subtitle: item.name }, (close) => (
        <ClosePeriodForm item={item} onDone={close} />
    ))
}

function ClosePeriodForm({ item, onDone }: { item: ExpenseItem; onDone: () => void }) {
    const { t } = useTranslation()
    const [month, setMonth] = useState<string | null>(dayjs().subtract(1, 'month').format('YYYY-MM-01'))
    const m = useApiMutation(() =>
        api.post<{ rub_amount: number }>(`expense-items/${item.id}/close-period`, { period: dayjs(month).format('YYYY-MM') })
    )
    return (
        <FormStack>
            <FormSection
                icon={PiCalendarCheckDuotone}
                color="orange"
                title={t('expense_items.period')}
                description={t('expense_items.close_hint')}
            >
                <MonthPickerInput label={t('expense_items.month')} leftSection={<PiCalendarDuotone size={16} />} value={month} onChange={setMonth} />
            </FormSection>
            <FormFooter
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    m.mutate(undefined, {
                        onSuccess: (r) => {
                            notifyOk(t('expense_items.booked', { amount: fmtMoney(r.rub_amount, 2) }))
                            onDone()
                        },
                        onError: (e) => notifyError(e)
                    })
                }
                submitIcon={<PiLock size={16} />}
                submitLabel={t('expense_items.close_period')}
            />
        </FormStack>
    )
}

function MeteredView({ item }: { item: ExpenseItem }) {
    const { t } = useTranslation()
    const [month, setMonth] = useState<string | null>(dayjs().format('YYYY-MM-01'))
    const q = useMetered(item.id, dayjs(month).format('YYYY-MM'))
    const consumers = useMemo<MRT_ColumnDef<NonNullable<NonNullable<typeof q.data>['top_consumers']>[number]>[]>(
        () => [
            { accessorKey: 'username', header: t('expense_items.col_user'), Cell: ({ row }) => row.original.username || `#${row.original.rw_user_id}` },
            { accessorKey: 'customer_name', header: t('expense_items.col_customer'), Cell: ({ row }) => row.original.customer_name || '–' },
            { accessorKey: 'gb', header: t('format.units.gb'), Cell: ({ cell }) => <Text className="num" size="sm">{fmtNum(cell.getValue<number>())}</Text> },
            { accessorKey: 'share_percent', header: t('expense_items.col_share'), Cell: ({ cell }) => `${fmtNum(cell.getValue<number>(), 1)}%` },
            { accessorKey: 'cost_rub', header: `≈ ${currencySymbol()}`, Cell: ({ cell }) => fmtMoney(cell.getValue<number>()) }
        ],
        [t]
    )
    return (
        <Stack>
            <MonthPickerInput label={t('expense_items.month')} leftSection={<PiCalendarDuotone size={16} />} value={month} onChange={setMonth} w={220} />
            {q.error && (
                <Alert color="red" variant="soft">
                    {q.error.message}
                </Alert>
            )}
            {q.data && <MeteredCard m={q.data} />}
            {q.data?.top_consumers && q.data.top_consumers.length > 5 && (
                <DataTableCard
                    compact
                    columns={consumers}
                    data={q.data.top_consumers}
                    icon={<PiUsersDuotone size={24} />}
                    initialState={{ sorting: [{ id: 'gb', desc: true }] }}
                    storageKey="consumers"
                    title={t('expense_items.all_consumers')}
                />
            )}
        </Stack>
    )
}

function ItemForm({ item, onDone }: { item: Partial<ExpenseItem>; onDone: () => void }) {
    const { t } = useTranslation()
    const nodes = useNodes()
    const infra = useInfra()
    const billing = usePanelBilling()
    const settings = useSettings()
    const itemsQ = useExpenseItems()
    const form = useForm({
        initialValues: {
            name: item.name ?? '',
            provider: item.provider ?? '',
            provider_uuid: item.provider_uuid ?? '',
            currency: item.currency ?? baseCurrency(),
            pricing: item.pricing ?? 'fixed',
            amount: item.amount ?? 0,
            period: item.period ?? 'month',
            fee_percent: item.fee_percent ?? 0,
            share_percent: item.share_percent ?? 100,
            price_per_gb: item.price_per_gb ?? 1,
            min_charge: item.min_charge ?? 0,
            gb_unit: item.gb_unit ?? 'binary',
            min_mode: item.min_mode ?? 'floor',
            free_gb: item.free_gb ?? 0,
            tiers: item.tiers ?? [],
            billing_day: item.billing_day ?? 1,
            rw_node_uuid: item.rw_node_uuid || null,
            rw_squad_uuid: item.rw_squad_uuid || null,
            next_due_date: item.next_due_date ? dayjs(item.next_due_date).format('YYYY-MM-DD') : null,
            active: item.active ?? true,
            notes: item.notes ?? ''
        },
        validate: { name: (v) => (v.trim() ? null : t('tariffs.name_required')) }
    })
    const m = useApiMutation((v: typeof form.values) => {
        const body = { ...v, rw_node_uuid: v.rw_node_uuid ?? '', rw_squad_uuid: v.rw_squad_uuid ?? '' }
        return item.id ? api.put(`expense-items/${item.id}`, body) : api.post('expense-items', body)
    })
    const v = form.values
    const knownProviders = useMemo(() => [...new Set((itemsQ.data?.items ?? []).map((i) => i.provider).filter(Boolean))], [itemsQ.data])
    const panelBilling = billing(v.rw_node_uuid)

    const pickNode = (val: string | null) => {
        form.setFieldValue('rw_node_uuid', val)
        // the panel already knows who hosts this node: take its provider
        const b = billing(val)
        const p = b && infra.data?.providers.find((x) => x.uuid === b.providerUuid)
        if (p && !v.provider) {
            form.setFieldValue('provider', p.name)
            form.setFieldValue('provider_uuid', p.uuid)
        }
    }

    return (
        <form
            onSubmit={form.onSubmit((vals) =>
                m.mutate(vals, {
                    onSuccess: () => {
                        notifyOk(t('common.saved'))
                        onDone()
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap="md">
                <FormSection icon={PiBuildingsDuotone} title={t('expense_items.item')} description={t('expense_items.item_hint')}>
                    <TextInput label={t('tariffs.col_name')} leftSection={<PiTextAa size={16} />} required {...form.getInputProps('name')} />
                    <ProviderInput
                        label={t('expense_items.provider')}
                        placeholder={t('expense_items.provider_placeholder')}
                        extra={knownProviders}
                        value={v.provider}
                        onChange={(name, uuid) => {
                            form.setFieldValue('provider', name)
                            form.setFieldValue('provider_uuid', uuid)
                        }}
                    />
                    <NodeSelect
                        label={t('expense_items.node')}
                        description={
                            panelBilling
                                ? t('expense_items.node_billing', { date: fmtDate(panelBilling.nextBillingAt) })
                                : t('expense_items.node_hint')
                        }
                        placeholder={nodes.isPending ? t('common.loading') : t('expense_items.not_selected')}
                        clearable
                        allowDeselect
                        value={v.rw_node_uuid}
                        onChange={pickNode}
                        error={nodes.error?.message}
                    />
                    <Switch label={t('expense_items.active')} description={t('expense_items.active_hint')} {...form.getInputProps('active', { type: 'checkbox' })} />
                </FormSection>

                <FormSection icon={PiCoinsDuotone} color="teal" title={t('expense_items.pricing')}>
                    <SegmentedControl
                        data={[
                            { value: 'fixed', label: t('expense_items.fixed') },
                            { value: 'metered', label: t('expense_items.metered') }
                        ]}
                        fullWidth
                        {...form.getInputProps('pricing')}
                    />
                    <SimpleGrid cols={{ base: 1, xs: 2 }}>
                        <SearchSelect
                            label={t('expense_items.currency')}
                            leftSection={<CurrencyIcon size={16} />}
                            data={CURRENCIES}
                            {...form.getInputProps('currency')}
                            onChange={(c) => {
                                form.setFieldValue('currency', c ?? baseCurrency())
                                if (c && c !== baseCurrency() && !v.fee_percent && settings.data) {
                                    form.setFieldValue('fee_percent', Number(settings.data.default_fee_percent) || 0)
                                }
                            }}
                        />
                        {v.pricing === 'fixed' ? (
                            <Select
                                label={t('expense_items.period')}
                                leftSection={<PiCalendarDuotone size={16} />}
                                data={[
                                    { value: 'month', label: t('expense_items.monthly_cap') },
                                    { value: 'year', label: t('expense_items.yearly_cap') }
                                ]}
                                {...form.getInputProps('period')}
                            />
                        ) : (
                            <NumberInput
                                label={t('expense_items.billing_day')}
                                description={t('expense_items.billing_day_hint')}
                                leftSection={<PiCalendarDuotone size={16} />}
                                min={1}
                                max={28}
                                allowDecimal={false}
                                {...form.getInputProps('billing_day')}
                            />
                        )}
                    </SimpleGrid>
                    {v.pricing === 'fixed' ? (
                        <NumberInput
                            label={t('expense_items.period_price', { currency: v.currency })}
                            leftSection={<PiCoinsDuotone size={16} />}
                            min={0}
                            decimalScale={2}
                            {...form.getInputProps('amount')}
                        />
                    ) : (
                        <MeteredPricingFields form={form} />
                    )}
                    <SimpleGrid cols={{ base: 1, xs: 2 }}>
                        <NumberInput
                            label={t('expense_items.fee')}
                            leftSection={<PiPercent size={16} />}
                            decimalScale={2}
                            {...form.getInputProps('fee_percent')}
                        />
                        <NumberInput
                            label={t('expense_items.share')}
                            description={t('expense_items.share_hint')}
                            leftSection={<PiPercent size={16} />}
                            min={0}
                            max={100}
                            decimalScale={2}
                            {...form.getInputProps('share_percent')}
                        />
                    </SimpleGrid>
                    <DateInput
                        label={t('expense_items.next_payment')}
                        description={panelBilling ? t('expense_items.next_payment_hint') : undefined}
                        leftSection={<PiCalendarDuotone size={16} />}
                        clearable
                        valueFormat={dateLayout()}
                        {...form.getInputProps('next_due_date')}
                    />
                </FormSection>

                {v.pricing === 'metered' && (
                    <FormSection icon={TbServer2} color="violet" title={t('expense_items.whose_traffic')} description={t('expense_items.whose_traffic_hint')}>
                        <SquadSelect
                            label={t('expense_items.squad')}
                            description={t('expense_items.squad_hint')}
                            placeholder={t('expense_items.whole_node')}
                            clearable
                            allowDeselect
                            {...form.getInputProps('rw_squad_uuid')}
                        />
                    </FormSection>
                )}

                <FormSection icon={PiNotePencil} color="gray" title={t('expense_items.notes')}>
                    <Textarea autosize minRows={2} {...form.getInputProps('notes')} />
                </FormSection>
                <FormFooter inline loading={m.isPending} onCancel={onDone} />
            </Stack>
        </form>
    )
}

type ItemFormValues = {
    currency: string
    price_per_gb: number
    min_charge: number
    gb_unit: GBUnit
    min_mode: MinMode
    free_gb: number
    tiers: Tier[]
}

// MeteredPricingFields edits how a per-GB provider bills: the GB unit, a
// minimum vs. a fixed fee with a free allowance, and flat vs. tiered prices.
function MeteredPricingFields<T extends ItemFormValues>({ form }: { form: UseFormReturnType<T> }) {
    const { t } = useTranslation()
    const v = form.getValues() as ItemFormValues
    const set = <K extends keyof ItemFormValues>(k: K, val: ItemFormValues[K]) => form.setFieldValue(k as never, val as never)
    const tiered = v.tiers.length > 0
    const free = v.min_mode === 'free'
    const firstPrice = tiered ? v.tiers[0].price_per_gb : v.price_per_gb
    return (
        <>
            <Select
                label={t('expense_items.gb_unit')}
                leftSection={<TbCloudDataConnection size={16} />}
                data={[
                    { value: 'binary', label: t('expense_items.gb_binary') },
                    { value: 'decimal', label: t('expense_items.gb_decimal') }
                ]}
                {...form.getInputProps('gb_unit')}
            />
            <SegmentedControl
                data={[
                    { value: 'floor', label: t('expense_items.min_charge') },
                    { value: 'free', label: t('expense_items.fee_allowance') }
                ]}
                fullWidth
                {...form.getInputProps('min_mode')}
            />
            <SimpleGrid cols={{ base: 1, xs: free ? 2 : 1 }}>
                <NumberInput
                    label={`${free ? t('expense_items.fee_label') : t('expense_items.min_charge')}, ${v.currency}`}
                    description={
                        free
                            ? t('expense_items.free_formula')
                            : t('expense_items.floor_formula', { gb: !tiered && firstPrice ? fmtNum(v.min_charge / firstPrice) : '—' })
                    }
                    leftSection={<PiCoinsDuotone size={16} />}
                    min={0}
                    decimalScale={2}
                    {...form.getInputProps('min_charge')}
                />
                {free && (
                    <NumberInput
                        label={t('expense_items.free_gb')}
                        leftSection={<TbCloudDataConnection size={16} />}
                        min={0}
                        decimalScale={2}
                        {...form.getInputProps('free_gb')}
                    />
                )}
            </SimpleGrid>
            <Switch
                label={t('expense_items.tiered')}
                description={t('expense_items.tiered_hint')}
                checked={tiered}
                onChange={(e) =>
                    set(
                        'tiers',
                        e.currentTarget.checked
                            ? [
                                  { up_to_gb: 10000, price_per_gb: v.price_per_gb },
                                  { up_to_gb: 0, price_per_gb: v.price_per_gb }
                              ]
                            : []
                    )
                }
            />
            {tiered ? (
                <Stack gap="xs">
                    {v.tiers.map((tier, i) => (
                        <Group align="flex-end" gap="xs" key={i} wrap="nowrap">
                            <NumberInput
                                label={i === 0 ? t('expense_items.tier_up_to') : undefined}
                                min={0}
                                decimalScale={2}
                                style={{ flex: 1 }}
                                value={tier.up_to_gb}
                                onChange={(x) => set('tiers', v.tiers.map((y, j) => (j === i ? { ...y, up_to_gb: Number(x) || 0 } : y)))}
                            />
                            <NumberInput
                                label={i === 0 ? t('expense_items.price_per_gb', { currency: v.currency }) : undefined}
                                min={0}
                                decimalScale={4}
                                style={{ flex: 1 }}
                                value={tier.price_per_gb}
                                onChange={(x) => set('tiers', v.tiers.map((y, j) => (j === i ? { ...y, price_per_gb: Number(x) || 0 } : y)))}
                            />
                            <ActionIcon
                                color="red"
                                mb={4}
                                onClick={() => set('tiers', v.tiers.filter((_, j) => j !== i))}
                                variant="subtle"
                            >
                                <PiTrash size={16} />
                            </ActionIcon>
                        </Group>
                    ))}
                    <Button
                        leftSection={<PiPlus size={14} />}
                        onClick={() => set('tiers', [...v.tiers, { up_to_gb: 0, price_per_gb: firstPrice }])}
                        size="compact-sm"
                        variant="subtle"
                        w="fit-content"
                    >
                        {t('expense_items.add_tier')}
                    </Button>
                </Stack>
            ) : (
                <NumberInput
                    label={t('expense_items.price_per_gb', { currency: v.currency })}
                    leftSection={<TbCloudDataConnection size={16} />}
                    min={0}
                    decimalScale={4}
                    {...form.getInputProps('price_per_gb')}
                />
            )}
        </>
    )
}
