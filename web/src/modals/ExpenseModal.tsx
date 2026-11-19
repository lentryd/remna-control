import { Alert, Group, NumberInput, Paper, Select, SimpleGrid, Switch, Text, TextInput } from '@mantine/core'
import { IconArrowBackUp, IconArrowUpRight, IconBuildingSkyscraper, IconCalendar, IconCoin, IconCoins, IconEdit, IconPercentage, IconReceipt } from '@tabler/icons-react'
import { DateInput } from '@mantine/dates'
import { useForm } from '@mantine/form'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useEffect, useRef } from 'react'

import { api } from '@/api/client'
import { useApiMutation, useExpenseItems, useExpenses, useSettings } from '@/api/hooks'
import type { Expense, ExpenseItem } from '@/api/types'
import { fmtDate, fmtMoney, baseCurrency, dateLayout } from '@/components/format'
import { notifyError, notifyOk } from '@/components/notify'
import { FormColumns, FormFooter, FormSection } from '@shared/ui/forms/form-section'
import { ProviderInput } from '@shared/ui/infra/provider'

import { openModal } from './open'
import { SearchSelect } from '@shared/ui/forms/search-select'
import { CurrencyIcon, CURRENCIES } from '@shared/currencies'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'


// ExpensePrefill starts a new expense as the payment of a recurring item
// (itemId) or of a panel provider.
interface ExpensePrefill {
    itemId?: number
    provider?: { name: string; uuid?: string }
    name?: string
}

export function openExpenseForm(p: { expense?: Expense; refundOf?: Expense } & ExpensePrefill) {
    const title = p.expense ? i18n.t('expenses.expense') : p.refundOf ? i18n.t('expense_modal.refund') : i18n.t('expense_modal.new')
    const subtitle = p.expense ? `${p.expense.provider} · ${dayjs(p.expense.date).format(dateLayout())}` : (p.refundOf?.provider ?? p.name)
    openModal({ icon: IconReceipt, color: 'orange', title, subtitle }, (close) => <ExpenseForm {...p} onDone={close} />, '1000px')
}

// nextDue is an item's payment date after one more period.
const nextDue = (it: ExpenseItem) => dayjs(it.next_due_date).add(1, it.period === 'year' ? 'year' : 'month').toISOString()

function ExpenseForm({
    expense,
    refundOf,
    itemId,
    provider,
    onDone
}: { expense?: Expense; refundOf?: Expense; onDone: () => void } & ExpensePrefill) {
    const { t } = useTranslation()
    const items = useExpenseItems()
    const all = useExpenses()
    const settings = useSettings()
    const src = expense ?? refundOf
    const form = useForm({
        initialValues: {
            date: expense ? dayjs(expense.date).format('YYYY-MM-DD') : dayjs().format('YYYY-MM-DD'),
            provider: src?.provider ?? provider?.name ?? '',
            provider_uuid: src?.provider_uuid ?? provider?.uuid ?? '',
            expense_item_id: src?.expense_item_id ? String(src.expense_item_id) : itemId ? String(itemId) : null,
            kind: expense?.kind ?? (refundOf ? 'refund' : 'charge'),
            orig_amount: expense?.orig_amount ?? 0,
            orig_currency: src?.orig_currency ?? baseCurrency(),
            fx_rate: expense?.fx_rate ?? (1 as number | ''),
            fee_percent: src?.fee_percent ?? 0,
            share_percent: src?.share_percent ?? 100,
            refund_of_id: expense?.refund_of_id ?? refundOf?.id ?? null,
            note: expense?.note ?? '',
            advance_due: true
        }
    })
    const v = form.values
    const item = items.data?.items.find((i) => String(i.id) === v.expense_item_id)
    // paying a dated item can move its next payment date one period on
    const canAdvance = !expense && v.kind === 'charge' && !!item?.next_due_date
    // a new expense for an item takes its price, currency and provider
    const fromItem = (it: ExpenseItem, cur: typeof v) => ({
        orig_currency: it.currency,
        fee_percent: it.fee_percent,
        share_percent: it.share_percent,
        provider: cur.provider || it.provider,
        provider_uuid: cur.provider ? cur.provider_uuid : it.provider_uuid,
        orig_amount: cur.orig_amount || it.amount
    })
    const prefilled = useRef(false)
    useEffect(() => {
        if (prefilled.current || !itemId || !items.data) return
        prefilled.current = true
        const it = items.data.items.find((i) => i.id === itemId)
        if (it) form.setValues(fromItem(it, form.getValues()))
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [items.data, itemId])
    const rate = useQuery({
        queryKey: ['fx', v.orig_currency, v.date],
        enabled: v.orig_currency !== baseCurrency() && !!v.date,
        queryFn: () => api.get<{ rate: number }>(`fx/rate?currency=${v.orig_currency}&date=${v.date}`)
    })
    useEffect(() => {
        if (v.orig_currency === baseCurrency()) form.setFieldValue('fx_rate', 1)
        else if (rate.data && !expense) form.setFieldValue('fx_rate', rate.data.rate)
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [rate.data, v.orig_currency])
    useEffect(() => {
        if (!expense && v.orig_currency !== baseCurrency() && v.fee_percent === 0 && settings.data) {
            form.setFieldValue('fee_percent', Number(settings.data.default_fee_percent) || 0)
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [v.orig_currency, settings.data])

    const rub = (Number(v.orig_amount) || 0) * (Number(v.fx_rate) || 0) * (1 + (Number(v.fee_percent) || 0) / 100) * ((Number(v.share_percent) || 0) / 100)
    const providers = Array.from(new Set((all.data ?? []).map((e) => e.provider).filter(Boolean))).sort()

    const m = useApiMutation((vals: typeof form.values) => {
        const body = {
            ...vals,
            expense_item_id: vals.expense_item_id ? Number(vals.expense_item_id) : null,
            fx_rate: vals.fx_rate === '' ? null : Number(vals.fx_rate),
            advance_due: canAdvance && vals.advance_due
        }
        return expense ? api.put(`expenses/${expense.id}`, body) : api.post('expenses', body)
    })

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
            <FormColumns
                left={
                    <FormSection icon={IconReceipt} color="orange" title={t('payment.title')} description={t('expense_modal.payment_hint')}>
                        <SimpleGrid cols={{ base: 1, xs: 2 }}>
                            <DateInput label={t('customer.col_date')} leftSection={<IconCalendar size={16} />} valueFormat={dateLayout()} {...form.getInputProps('date')} />
                            <Select
                                label={t('backup.col_kind')}
                                leftSection={v.kind === 'refund' ? <IconArrowBackUp size={16} /> : <IconArrowUpRight size={16} />}
                                data={[
                                    { value: 'charge', label: t('expenses.charge') },
                                    { value: 'refund', label: t('expense_modal.refund') }
                                ]}
                                {...form.getInputProps('kind')}
                            />
                        </SimpleGrid>
                        <ProviderInput
                            label={t('expense_items.provider')}
                            placeholder={t('expense_items.provider_placeholder')}
                            extra={providers}
                            value={v.provider}
                            onChange={(name, uuid) => form.setValues({ provider: name, provider_uuid: uuid })}
                        />
                        <Select
                            label={t('expense_items.item_title')}
                            leftSection={<IconBuildingSkyscraper size={16} />}
                            placeholder={t('expense_modal.no_item')}
                            clearable
                            allowDeselect
                            data={(items.data?.items ?? []).map((i) => ({ value: String(i.id), label: i.name }))}
                            {...form.getInputProps('expense_item_id')}
                            onChange={(val) => {
                                form.setFieldValue('expense_item_id', val)
                                const it = items.data?.items.find((i) => String(i.id) === val)
                                if (it && !expense) form.setValues(fromItem(it, v))
                            }}
                        />
                        {canAdvance && (
                            <Switch
                                description={t('expense_modal.advance_due_hint', { from: fmtDate(item!.next_due_date), to: fmtDate(nextDue(item!)) })}
                                label={t('expense_modal.advance_due')}
                                {...form.getInputProps('advance_due', { type: 'checkbox' })}
                            />
                        )}
                        {v.kind === 'refund' && (
                            <SearchSelect
                                label={t('expense_modal.refund_of')}
                                description={t('expense_modal.refund_of_hint')}
                                leftSection={<IconArrowBackUp size={16} />}
                                clearable
                                allowDeselect
                                data={(all.data ?? [])
                                    .filter((e) => e.kind === 'charge')
                                    .map((e) => ({
                                        value: String(e.id),
                                        label: `${dayjs(e.date).format(dateLayout())} · ${e.provider} · ${fmtMoney(e.rub_amount, 2)}`
                                    }))}
                                value={v.refund_of_id ? String(v.refund_of_id) : null}
                                onChange={(val) => form.setFieldValue('refund_of_id', val ? Number(val) : null)}
                            />
                        )}
                    </FormSection>
                }
                right={
                    <>
                        <FormSection icon={IconCoins} color="teal" title={t('customer.col_amount')} description={t('expense_modal.amount_hint', { currency: baseCurrency() })}>
                            <SimpleGrid cols={{ base: 1, xs: 2 }}>
                                <NumberInput label={t('customer.col_amount')} leftSection={<IconCoins size={16} />} min={0} decimalScale={2} {...form.getInputProps('orig_amount')} />
                                <SearchSelect
                                    label={t('expense_items.currency')}
                                    leftSection={<IconCoin size={16} />}
                                    data={CURRENCIES}
                                    {...form.getInputProps('orig_currency')}
                                />
                            </SimpleGrid>
                            {v.orig_currency !== baseCurrency() && (
                                <SimpleGrid cols={{ base: 1, xs: 2 }}>
                                    <NumberInput
                                        label={t('expense_modal.rate', { base: baseCurrency(), currency: v.orig_currency })}
                                        description={rate.isFetching ? t('common.loading') : t('expense_modal.rate_hint')}
                                        leftSection={<CurrencyIcon size={16} />}
                                        decimalScale={4}
                                        min={0}
                                        {...form.getInputProps('fx_rate')}
                                    />
                                    <NumberInput
                                        label={t('expense_items.fee')}
                                        description={t('expense_modal.fee_hint')}
                                        leftSection={<IconPercentage size={16} />}
                                        decimalScale={2}
                                        {...form.getInputProps('fee_percent')}
                                    />
                                </SimpleGrid>
                            )}
                            <NumberInput
                                label={t('expense_items.share')}
                                description={t('expense_items.share_hint')}
                                leftSection={<IconPercentage size={16} />}
                                min={0}
                                max={100}
                                decimalScale={2}
                                {...form.getInputProps('share_percent')}
                            />
                            {rate.error && (
                                <Alert color="red" variant="soft">
                                    {rate.error.message}
                                </Alert>
                            )}
                            <Paper bd="1px solid color-mix(in srgb, var(--mantine-color-orange-6) 24%, transparent)" bg="var(--mantine-color-orange-light)" p="sm" radius="md">
                                <Group justify="space-between">
                                    <Text c="dimmed" size="sm">
                                        {t('expense_modal.total', { currency: baseCurrency() })}
                                    </Text>
                                    <Text c={v.kind === 'refund' ? 'teal' : 'orange'} className="num" fw={600}>
                                        {v.kind === 'refund' ? '−' : ''}
                                        {fmtMoney(rub, 2)}
                                    </Text>
                                </Group>
                            </Paper>
                        </FormSection>
                        <FormSection icon={IconEdit} color="gray" title={t('payment.comment')}>
                            <TextInput placeholder={t('expense_modal.note_placeholder')} {...form.getInputProps('note')} />
                        </FormSection>
                    </>
                }
            />
            <FormFooter loading={m.isPending} onCancel={onDone} />
        </form>
    )
}
