import { Alert, Badge, Group, Loader, NumberInput, Paper, SimpleGrid, Stack, Switch, Text, TextInput, ThemeIcon, Autocomplete } from '@mantine/core'
import { DateInput } from '@mantine/dates'
import { useDebouncedValue } from '@mantine/hooks'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { IconBinaryTree, IconBuildingBank, IconCalendar, IconCalendarPlus, IconCheck, IconCircleCheck, IconClock, IconCreditCard, IconEdit, IconHexagon, IconPuzzle, IconUser, IconX } from '@tabler/icons-react'
import dayjs from 'dayjs'
import { useEffect, useState } from 'react'

import { api } from '@/api/client'
import { useApiMutation, useCustomers } from '@/api/hooks'
import type { ExtensionResult, PaymentPreview, PlanItem, ReferralInfo } from '@/api/types'
import { durationLabel, fmtDate, fmtMoney, currencySymbol, dateLayout } from '@/components/format'
import { notifyError } from '@/components/notify'
import { periodCost } from '@/components/pricing'
import { StatStrip } from '@shared/ui/stat-strip'
import { FormFooter, FormSection, FormStack } from '@shared/ui/forms/form-section'
import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'
import { SearchSelect } from '@shared/ui/forms/search-select'

import { openModal } from './open'
import { CurrencyIcon } from '@shared/currencies'
import i18n from '@/app/i18n/i18n'
import { paymentMethods } from '@/modals/PaymentEditModal'
import { useTranslation } from 'react-i18next'

// openPaymentModal records a customer's payment and spreads it over their
// subscriptions. Without a customer the form starts by picking one.
export function openPaymentModal(p: { customerId?: number; name?: string } = {}) {
    openModal(
        { icon: IconCreditCard, color: 'teal', title: i18n.t('payment.title'), subtitle: p.name },
        (close) => <PaymentForm customerId={p.customerId} onDone={close} />,
        'xl'
    )
}

interface Row {
    key: string
    item: PlanItem
    months: number
    days: number
    amount: number
}

function PaymentForm({ customerId: fixedCustomer, onDone }: { customerId?: number; onDone: () => void }) {
    const { t } = useTranslation()
    const customers = useCustomers(!fixedCustomer)
    const [picked, setPicked] = useState<string | null>(null)
    const customerId = fixedCustomer ?? (picked ? Number(picked) : null)
    const [amount, setAmount] = useState<number>(0)
    const [date, setDate] = useState<string | null>(dayjs().format('YYYY-MM-DD'))
    const [method, setMethod] = useState<string>(() => paymentMethods()[0])
    const [note, setNote] = useState('')
    const [toDays, setToDays] = useState(false)
    const [rows, setRows] = useState<Row[]>([])
    const [results, setResults] = useState<{ ext: ExtensionResult[]; balance: number; ref: ReferralInfo | null } | null>(null)
    const [debouncedAmount] = useDebouncedValue(amount, 300)

    // The split is recalculated as the amount changes; edits to the rows
    // hold until the next recalculation.
    const preview = useQuery({
        queryKey: ['payment-preview', customerId, debouncedAmount, toDays],
        queryFn: () => api.post<PaymentPreview>(`customers/${customerId}/payments/preview`, { amount: debouncedAmount, remainder_to_days: toDays }),
        enabled: !!customerId && debouncedAmount > 0,
        placeholderData: keepPreviousData,
        staleTime: Infinity,
        gcTime: 0
    })
    const p = customerId && amount > 0 ? preview.data : undefined
    useEffect(() => {
        if (!preview.data) return
        const d = preview.data
        setRows(
            [...d.items]
                .sort((a, b) => Number(b.auto_extend) - Number(a.auto_extend))
                .map((it) => {
                    const a = d.allocations.find((x) => x.kind === it.kind && x.id === it.id)
                    return { key: `${it.kind}-${it.id}`, item: it, months: a?.months ?? 0, days: a?.days ?? 0, amount: a?.amount ?? 0 }
                })
        )
    }, [preview.data])

    const commit = useApiMutation(() =>
        api.post<{ extensions: ExtensionResult[]; balance: number; referral: ReferralInfo | null }>(`customers/${customerId}/payments`, {
            amount,
            date,
            method: method ?? '',
            note,
            allocations: rows
                .filter((r) => r.months + r.days > 0)
                .map((r) => ({ kind: r.item.kind, id: r.item.id, months: r.months, days: r.days, amount: r.amount }))
        })
    )

    const update = (key: string, patch: Partial<Row>) =>
        setRows((rs) =>
            rs.map((r) => {
                if (r.key !== key) return r
                const next = { ...r, ...patch }
                if (patch.months !== undefined || patch.days !== undefined) next.amount = periodCost(next.item.monthly, next.item.periods, next.months, next.days)
                return next
            })
        )

    if (results) {
        return (
            <FormStack>
                <FormSection icon={IconCircleCheck} color="teal" title={t('payment.recorded')} description={t('payment.balance', { amount: fmtMoney(results.balance, 2) })}>
                    {results.ref && (
                        <Badge color="grape" leftSection={<IconBinaryTree size={14} />} size="lg" variant="soft">
                            {t('payment.ref_result', { name: results.ref.referrer_name, amount: fmtMoney(results.ref.amount, 2), pct: results.ref.percent })}
                        </Badge>
                    )}
                    {results.ext.length === 0 && (
                        <Text c="dimmed" size="sm">
                            {t('payment.no_extensions')}
                        </Text>
                    )}
                    {results.ext.map((e) => (
                        <Group gap="xs" key={`${e.kind}-${e.id}`} wrap="nowrap">
                            <ThemeIcon color={e.ok ? 'teal' : 'red'} size="md" variant="soft">
                                {e.ok ? <IconCheck size={14} /> : <IconX size={14} />}
                            </ThemeIcon>
                            <Text size="sm">
                                {e.title}: {durationLabel(e.months, e.days)}
                                {e.ok ? ` → ${t('view.until', { date: fmtDate(e.to) })}` : ` — ${t('payment.ext_failed', { error: e.error })}`}
                            </Text>
                        </Group>
                    ))}
                </FormSection>
                <FormFooter onSubmit={onDone} submitIcon={<IconCheck size={16} />} submitLabel={t('common.done')} />
            </FormStack>
        )
    }

    const allocated = rows.reduce((s, r) => s + r.amount, 0)
    const rest = p ? Math.round((p.balance_after_payment - allocated) * 100) / 100 : 0
    // the shown split must match the amount being recorded
    const settled = !!p && debouncedAmount === amount && !preview.isFetching

    return (
        <FormStack>
            <FormSection icon={IconCreditCard} color="teal" title={t('payment.income')} description={t('payment.income_hint')}>
                {!fixedCustomer && (
                    <SearchSelect
                        data={(customers.data ?? []).filter((c) => !c.archived).map((c) => ({ value: String(c.id), label: c.name }))}
                        data-autofocus
                        label={t('sub.customer')}
                        leftSection={<IconUser size={16} />}
                        onChange={setPicked}
                        placeholder={t('payment.pick_customer')}
                        value={picked}
                    />
                )}
                <SimpleGrid cols={{ base: 1, xs: 2 }}>
                    <NumberInput
                        label={t('common.amount_in', { currency: currencySymbol() })}
                        leftSection={<CurrencyIcon size={16} />}
                        rightSection={preview.isFetching ? <Loader size={14} /> : undefined}
                        min={0}
                        decimalScale={2}
                        value={amount || ''}
                        onChange={(v) => setAmount(Number(v) || 0)}
                        data-autofocus={fixedCustomer ? true : undefined}
                    />
                    <DateInput label={t('customer.col_date')} leftSection={<IconCalendar size={16} />} value={date} onChange={setDate} valueFormat={dateLayout()} />
                    <Autocomplete label={t('payment.method')} leftSection={<IconBuildingBank size={16} />} data={paymentMethods()} value={method} onChange={setMethod} />
                    <TextInput label={t('payment.comment')} leftSection={<IconEdit size={16} />} value={note} onChange={(e) => setNote(e.currentTarget.value)} />
                </SimpleGrid>
                <Switch
                    label={t('payment.rest_to_days')}
                    description={t('payment.rest_to_days_hint')}
                    checked={toDays}
                    onChange={(e) => setToDays(e.currentTarget.checked)}
                />
            </FormSection>

            {p && (
                <>
                    <StatStrip
                        items={[
                            { label: t('payment.balance_before'), value: fmtMoney(p.balance_before, 2) },
                            { label: t('payment.after_payment'), value: fmtMoney(p.balance_after_payment, 2), color: 'teal' },
                            { label: t('payment.remains'), value: fmtMoney(rest, 2), color: rest < 0 ? 'red' : undefined }
                        ]}
                    />
                    {p.referral && (
                        <Badge color="grape" leftSection={<IconBinaryTree size={14} />} size="lg" variant="soft">
                            {t('payment.ref_preview', { name: p.referral.referrer_name, amount: fmtMoney(p.referral.amount, 2), pct: p.referral.percent })}
                        </Badge>
                    )}
                    <FormSection icon={IconCalendarPlus} color="teal" title={t('sub.renewal')} description={t('payment.renewal_hint')}>
                        {rows.length === 0 && (
                            <Alert color="yellow" variant="soft">
                                {t('payment.no_items')}
                            </Alert>
                        )}
                        {rows.map((r) => (
                            <AllocationRow key={r.key} onChange={(patch) => update(r.key, patch)} row={r} />
                        ))}
                        {rest < 0 && (
                            <Alert color="orange" variant="soft">
                                {t('payment.debt_warning', { amount: fmtMoney(-rest, 2) })}
                            </Alert>
                        )}
                    </FormSection>
                </>
            )}

            <FormFooter
                disabled={!customerId || amount <= 0 || !date || !settled}
                loading={commit.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    commit.mutate(undefined, {
                        onSuccess: (r) => setResults({ ext: r.extensions, balance: r.balance, ref: r.referral }),
                        onError: (e) => notifyError(e)
                    })
                }
                submitIcon={<IconCheck size={16} />}
                submitLabel={t('payment.commit')}
            />
        </FormStack>
    )
}

function AllocationRow({ row: r, onChange }: { row: Row; onChange: (patch: Partial<Row>) => void }) {
    const { t } = useTranslation()
    const base = r.item.expire_at && dayjs(r.item.expire_at).isAfter(dayjs()) ? dayjs(r.item.expire_at) : dayjs()
    const active = r.months + r.days > 0
    return (
        <Paper bd="1px solid var(--app-border)" bg="var(--app-surface-2)" p="sm" radius="md" style={{ opacity: active || r.item.auto_extend ? 1 : 0.75 }}>
            <Stack gap="xs">
                <Group justify="space-between" wrap="nowrap">
                    <BaseOverlayHeader
                        iconColor={r.item.kind === 'addon' ? 'grape' : 'brand'}
                        IconComponent={r.item.kind === 'addon' ? IconPuzzle : IconHexagon}
                        subtitle={t('payment.item_subtitle', { monthly: fmtMoney(r.item.monthly), date: fmtDate(r.item.expire_at) })}
                        title={r.item.title}
                        titleOrder={6}
                    />
                    <Group gap={6} wrap="nowrap">
                        {!r.item.auto_extend && (
                            <Badge color="gray" size="sm" variant="soft">
                                {t('payment.manual')}
                            </Badge>
                        )}
                        <Badge color={active ? 'teal' : 'gray'} leftSection={<IconCalendar size={14} />} size="lg" variant="soft">
                            {active ? fmtDate(base.add(r.months, 'month').add(r.days, 'day').toISOString()) : t('payment.no_extension')}
                        </Badge>
                    </Group>
                </Group>
                <SimpleGrid cols={3} spacing="xs">
                    <NumberInput
                        label={t('tariffs.months')}
                        leftSection={<IconCalendar size={14} />}
                        min={0}
                        max={36}
                        size="xs"
                        value={r.months}
                        onChange={(v) => onChange({ months: Number(v) || 0 })}
                    />
                    <NumberInput
                        label={t('tariffs.days')}
                        leftSection={<IconClock size={14} />}
                        min={0}
                        max={365}
                        size="xs"
                        value={r.days}
                        onChange={(v) => onChange({ days: Number(v) || 0 })}
                    />
                    <NumberInput
                        label={t('common.amount_in', { currency: currencySymbol() })}
                        leftSection={<CurrencyIcon size={14} />}
                        min={0}
                        decimalScale={2}
                        size="xs"
                        value={r.amount}
                        onChange={(v) => onChange({ amount: Number(v) || 0 })}
                    />
                </SimpleGrid>
            </Stack>
        </Paper>
    )
}
