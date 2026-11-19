// Term and payment cards shared by the extend, provision and add-on
// modals, styled after the panel's access-settings card (date + presets).
import { Autocomplete, Box, Button, Group, NumberInput, Paper, SegmentedControl, SimpleGrid, Stack, Switch, Text } from '@mantine/core'
import { DateInput, DatePickerInput } from '@mantine/dates'
import dayjs, { type Dayjs } from 'dayjs'
import type { ReactNode } from 'react'
import { IconArrowRight, IconBuildingBank, IconCalendar, IconCalendarPlus, IconClock, IconCreditCard, IconWallet } from '@tabler/icons-react'

import type { Period } from '@/api/types'
import { durationLabel, fmtDate, fmtMoney, currencySymbol } from '@/components/format'
import { periodCost } from '@/components/pricing'
import { FormSection } from '@shared/ui/forms/form-section'
import { CurrencyIcon } from '@shared/currencies'
import { dateLayout } from './format'
import { useTranslation } from 'react-i18next'
import { paymentMethods } from '@/modals/PaymentEditModal'

// Term is months+days from the base date; until pins the exact end (e.g.
// "to the end of the subscription"), months/days then only price it.
export interface Term {
    months: number
    days: number
    until: string | null
}

export interface TermPreset {
    label: string
    hint?: string
    term: Term
}

// termTo is where the term ends.
export function termTo(from: Dayjs, t: Term) {
    return t.until ? dayjs(t.until) : from.add(t.months, 'month').add(t.days, 'day')
}

// termBetween is the months+days from `from` to `to`, days rounded up.
export function termBetween(from: Dayjs, to: Dayjs): Term {
    const months = Math.max(0, to.diff(from, 'month'))
    const days = Math.max(0, Math.ceil(to.diff(from.add(months, 'month'), 'day', true) - 1e-6))
    return { months, days, until: null }
}

// tariffPresets offers a plain month and every tariff period, with prices.
export function tariffPresets(monthly: number | undefined, periods: Period[] | null | undefined): TermPreset[] {
    const terms = [{ months: 1, days: 0 }, ...(periods ?? [])]
    const seen = new Set<string>()
    return terms
        .filter((t) => {
            const k = `${t.months}:${t.days ?? 0}`
            return !seen.has(k) && seen.add(k)
        })
        .sort((a, b) => a.months * 31 + (a.days ?? 0) - (b.months * 31 + (b.days ?? 0)))
        .map((t) => ({
            label: durationLabel(t.months, t.days ?? 0),
            hint: monthly === undefined ? undefined : fmtMoney(periodCost(monthly, periods, t.months, t.days ?? 0)),
            term: { months: t.months, days: t.days ?? 0, until: null }
        }))
}

const sameTerm = (a: Term, b: Term) => a.months === b.months && a.days === b.days && (a.until ?? null) === (b.until ?? null)

export function TermSection({
    from,
    value,
    onChange,
    presets,
    description,
    children
}: {
    from: string | null | undefined
    value: Term
    onChange: (t: Term) => void
    presets: TermPreset[]
    description?: ReactNode
    children?: ReactNode
}) {
    const { t } = useTranslation()
    const base = from ? dayjs(from) : dayjs()
    const to = termTo(base, value)
    const empty = value.months + value.days <= 0 && !value.until

    return (
        <FormSection color="teal" description={description ?? t('term.from_expiry')} icon={IconCalendarPlus} title={t('dashboard.col_term')}>
            {presets.length > 0 && (
                <Group gap={6}>
                    {presets.map((p) => {
                        const active = sameTerm(p.term, value)
                        return (
                            <Button
                                color={active ? 'teal' : 'gray'}
                                key={p.label}
                                onClick={() => onChange(p.term)}
                                radius="md"
                                rightSection={
                                    p.hint && (
                                        <Text c={active ? 'teal.3' : 'dimmed'} className="num" size="xs">
                                            {p.hint}
                                        </Text>
                                    )
                                }
                                size="xs"
                                variant={active ? 'light' : 'default'}
                            >
                                {p.label}
                            </Button>
                        )
                    })}
                </Group>
            )}
            <SimpleGrid cols={{ base: 1, xs: 2 }}>
                <NumberInput
                    label={t('tariffs.months')}
                    leftSection={<IconCalendar size={16} />}
                    max={36}
                    min={0}
                    onChange={(v) => onChange({ months: Number(v) || 0, days: value.days, until: null })}
                    value={value.months}
                />
                <NumberInput
                    label={t('tariffs.days')}
                    leftSection={<IconClock size={16} />}
                    max={365}
                    min={0}
                    onChange={(v) => onChange({ months: value.months, days: Number(v) || 0, until: null })}
                    value={value.days}
                />
            </SimpleGrid>
            <DatePickerInput
                description={t('term.pick_date')}
                label={t('term.end_date')}
                leftSection={<IconCalendar size={16} />}
                minDate={base.add(1, 'day').format('YYYY-MM-DD')}
                onChange={(d) => {
                    if (!d) return
                    const target = dayjs(d).hour(base.hour()).minute(base.minute()).second(base.second())
                    onChange(termBetween(base, target))
                }}
                value={empty ? null : to.format('YYYY-MM-DD')}
                valueFormat={dateLayout()}
            />
            <Group className="term-range" gap="sm" justify="space-between" wrap="nowrap">
                <Box>
                    <Text c="dimmed" size="xs">
                        {t('term.from')}
                    </Text>
                    <Text className="num" fw={500} size="sm">
                        {fmtDate(base.toISOString())}
                    </Text>
                </Box>
                <IconArrowRight color="var(--app-text-faint)" size={18} style={{ flex: '0 0 auto' }} />
                <Box ta="right">
                    <Text c="dimmed" size="xs">
                        {t('term.to')}
                    </Text>
                    <Text c={empty ? 'dimmed' : 'teal'} className="num" fw={600} size="sm">
                        {empty ? '—' : fmtDate(to.toISOString())}
                    </Text>
                </Box>
            </Group>
            {children}
        </FormSection>
    )
}

function SummaryRow({ label, children }: { label: string; children: ReactNode }) {
    return (
        <Group justify="space-between" wrap="nowrap">
            <Text c="dimmed" size="sm">
                {label}
            </Text>
            <Text component="div" className="num" fw={600} size="sm">
                {children}
            </Text>
        </Group>
    )
}

// Source is where an extension's money comes from: a payment the customer
// just made (recorded together with the extension) or their balance.
export type Source = 'paid' | 'balance'

export function SourceSwitch({ value, onChange }: { value: Source; onChange: (v: Source) => void }) {
    const { t } = useTranslation()
    return (
        <SegmentedControl
            data={[
                { value: 'paid', label: t('term.source_paid') },
                { value: 'balance', label: t('term.source_balance') }
            ]}
            fullWidth
            onChange={(v) => onChange(v as Source)}
            value={value}
        />
    )
}

// Income is a payment recorded together with an extension.
export interface Income {
    amount: number
    date: string | null
    method: string
}

export const newIncome = (): Income => ({ amount: 0, date: dayjs().format('YYYY-MM-DD'), method: paymentMethods()[0] })

// incomeFor is what the customer has to pay for a charge, given what the
// balance already covers.
export const incomeFor = (charge: number, balance: number) => Math.max(0, Math.round((charge - Math.max(balance, 0)) * 100) / 100)

export function IncomeSection({ value, onChange }: { value: Income; onChange: (v: Income) => void }) {
    const { t } = useTranslation()
    return (
        <FormSection color="teal" description={t('term.income_hint')} icon={IconCreditCard} title={t('payment.income')}>
            <NumberInput
                decimalScale={2}
                label={t('common.amount_in', { currency: currencySymbol() })}
                leftSection={<CurrencyIcon size={16} />}
                min={0}
                onChange={(v) => onChange({ ...value, amount: Number(v) || 0 })}
                value={value.amount || ''}
            />
            <SimpleGrid cols={{ base: 1, xs: 2 }}>
                <DateInput
                    label={t('customer.col_date')}
                    leftSection={<IconCalendar size={16} />}
                    onChange={(d) => onChange({ ...value, date: d })}
                    value={value.date}
                    valueFormat={dateLayout()}
                />
                <Autocomplete
                    data={paymentMethods()}
                    label={t('payment.method')}
                    leftSection={<IconBuildingBank size={16} />}
                    onChange={(m) => onChange({ ...value, method: m })}
                    value={value.method}
                />
            </SimpleGrid>
        </FormSection>
    )
}

export function PaymentSection({
    amount,
    onAmount,
    allowDebt,
    onAllowDebt,
    balance,
    monthly,
    income,
    description
}: {
    amount: number | null
    onAmount: (v: number) => void
    allowDebt: boolean
    onAllowDebt: (v: boolean) => void
    balance?: number
    monthly?: number
    // a payment recorded along with the charge: it lands on the balance
    // first, and the charge may then take the balance below zero
    income?: number
    description?: string
}) {
    const { t } = useTranslation()
    const after = balance === undefined ? undefined : balance + (income ?? 0) - (amount ?? 0)
    const short = after !== undefined && after < 0 && !allowDebt
    return (
        <FormSection color="orange" description={t('term.charged_hint')} icon={IconWallet} title={t('term.payment')}>
            <NumberInput
                decimalScale={2}
                description={description ?? t('term.amount_hint')}
                label={t('term.charge', { currency: currencySymbol() })}
                leftSection={<CurrencyIcon size={16} />}
                min={0}
                onChange={(v) => onAmount(Number(v) || 0)}
                value={amount ?? ''}
            />
            {(balance !== undefined || monthly !== undefined) && (
                <Paper bd="1px solid var(--app-border)" bg="var(--app-surface-2)" p="sm" radius="md">
                    <Stack gap={6}>
                        {monthly !== undefined && <SummaryRow label={t('tariffs.monthly_price')}>{fmtMoney(monthly)}</SummaryRow>}
                        {balance !== undefined && <SummaryRow label={t('term.balance_now')}>{fmtMoney(balance, 2)}</SummaryRow>}
                        {income !== undefined && (
                            <SummaryRow label={t('payment.income')}>
                                <Text c="teal.5" className="num" fw={600} size="sm">
                                    +{fmtMoney(income, 2)}
                                </Text>
                            </SummaryRow>
                        )}
                        {after !== undefined && (
                            <SummaryRow label={t('term.after_charge')}>
                                <Text c={after < 0 ? 'red.5' : 'teal.5'} className="num" fw={600} size="sm">
                                    {fmtMoney(after, 2)}
                                </Text>
                            </SummaryRow>
                        )}
                    </Stack>
                </Paper>
            )}
            {income === undefined ? (
                <Switch
                    checked={allowDebt}
                    description={short ? t('term.short', { amount: fmtMoney(-after!, 2) }) : t('term.debt')}
                    label={t('term.allow_debt')}
                    onChange={(e) => onAllowDebt(e.currentTarget.checked)}
                    styles={short ? { description: { color: 'var(--mantine-color-red-5)' } } : undefined}
                />
            ) : (
                after !== undefined &&
                after < 0 && (
                    <Text c="orange.5" size="xs">
                        {t('term.income_short', { amount: fmtMoney(-after, 2) })}
                    </Text>
                )
            )}
        </FormSection>
    )
}
