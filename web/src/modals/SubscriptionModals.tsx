import { Alert, Badge, Group, Indicator, NumberInput, Paper, SegmentedControl, Select, type SelectProps, SimpleGrid, Stack, Switch, Text, TextInput } from '@mantine/core'
import { DateTimePicker } from '@mantine/dates'
import { TbUser } from 'react-icons/tb'
import {
    PiArrowsClockwise,
    PiArrowsClockwiseDuotone,
    PiArrowsLeftRight,
    PiArrowsLeftRightDuotone,
    PiCalendarDuotone,
    PiLinkDuotone,
    PiPlusCircle,
    PiPlusCircleDuotone,
    PiPuzzlePiece,
    PiPuzzlePieceDuotone,
    PiTag,
    PiTagDuotone,
    PiUser,
    PiUserCircle,
    PiUserCircleDuotone,
    PiWalletDuotone
} from 'react-icons/pi'
import { useForm } from '@mantine/form'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useEffect, useMemo, useState } from 'react'

import { api } from '@/api/client'
import { useApiMutation, useCustomers, useRwUsers, useTariffs } from '@/api/hooks'
import type { RwUserRow, Subscription, Tariff, TariffQuote } from '@/api/types'
import { expiryColor, StatusBadge } from '@/components/badges'
import { daysLeft, fmtBytes, fmtDate, fmtMoney, isUnlimited, currencySymbol } from '@/components/format'
import { errorText, notifyError, notifyOk } from '@/components/notify'
import { periodCost } from '@/components/pricing'
import {
    type Income,
    IncomeSection,
    incomeFor,
    newIncome,
    PaymentSection,
    type Source,
    SourceSwitch,
    tariffPresets,
    type Term,
    termBetween,
    type TermPreset,
    TermSection
} from '@/components/term'
import { FormColumns, FormFooter, FormSection } from '@shared/ui/forms/form-section'

import { openModal } from './open'
import { SearchSelect } from '@shared/ui/forms/search-select'
import { CurrencyIcon } from '@shared/currencies'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

export const tariffLabel = (t: Tariff) =>
    i18n.t(t.active ? 'sub.tariff_label' : 'sub.tariff_label_inactive', { name: t.name, price: fmtMoney(t.monthly_price) })

export const rwLabel = (u: RwUserRow) => `${u.username}${u.description ? ` · ${u.description}` : ''}`

const statusColor: Record<string, string> = { ACTIVE: 'teal', EXPIRED: 'red', LIMITED: 'orange', DISABLED: 'gray' }

// openSubscriptionForm links a panel user to a customer; an existing link
// is edited in the subscription modal itself.
export function openSubscriptionForm(p: { customerId?: number; rwUserId?: number }) {
    openModal({ icon: PiLinkDuotone, color: 'brand', title: i18n.t('sub.link_title') }, (close) => <SubscriptionForm {...p} onDone={close} />, '1000px')
}

// RwUserSelect picks a panel user, listed the way the panel shows them.
export function RwUserSelect({
    options,
    users,
    ...props
}: { options: { value: string; label: string }[]; users: RwUserRow[] | undefined } & Omit<SelectProps, 'data'>) {
    const { t } = useTranslation()
    return (
        <SearchSelect
            allowDeselect
            clearable
            data={options}
            label={t('sub.rw_user')}
            leftSection={<PiUserCircle size={16} />}
            nothingFoundMessage={t('sub.no_free_users')}
            placeholder={t('sub.choose_user')}
            renderOption={({ option }) => {
                const u = users?.find((x) => String(x.id) === option.value)
                return (
                    <Group gap="sm" wrap="nowrap">
                        <Indicator color={statusColor[u?.status ?? ''] ?? 'gray'} inline size={8} zIndex={0} />
                        <Text size="sm">{u?.username}</Text>
                        {u?.description && (
                            <Text c="dimmed" size="xs" truncate="end">
                                {u.description}
                            </Text>
                        )}
                    </Group>
                )
            }}
            {...props}
        />
    )
}

// RwUserPreview shows the picked panel user the way the panel lists it.
function RwUserPreview({ user }: { user: RwUserRow }) {
    const { t } = useTranslation()
    const days = daysLeft(user.expire_at)
    return (
        <Paper bd="1px solid var(--app-border)" bg="var(--app-surface-2)" p="sm" radius="md">
            <Stack gap="sm">
                <Group justify="space-between" wrap="nowrap">
                    <Stack gap={0} miw={0}>
                        <Text fw={600} size="sm" truncate="end">
                            {user.username}
                        </Text>
                        <Text c="dimmed" size="xs" truncate="end">
                            {user.description || t('sub.no_description')}
                        </Text>
                    </Stack>
                    <StatusBadge size="md" user={user} />
                </Group>
                <SimpleGrid cols={2} spacing="sm">
                    <Stack gap={0}>
                        <Text c="dimmed" size="xs">
                            {t('sub.expires')}
                        </Text>
                        <Text c={expiryColor(days)} className="num" fw={500} size="sm">
                            {fmtDate(user.expire_at)}
                        </Text>
                    </Stack>
                    <Stack gap={0}>
                        <Text c="dimmed" size="xs">
                            {t('sub.traffic')}
                        </Text>
                        <Text className="num" fw={500} size="sm">
                            {fmtBytes(user.used_traffic_bytes)} / {user.traffic_limit_bytes ? fmtBytes(user.traffic_limit_bytes) : '∞'}
                        </Text>
                    </Stack>
                </SimpleGrid>
            </Stack>
        </Paper>
    )
}

function SubscriptionForm({ customerId, rwUserId, onDone }: { customerId?: number; rwUserId?: number; onDone: () => void }) {
    const { t } = useTranslation()
    const customers = useCustomers()
    const tariffs = useTariffs()
    const rwUsers = useRwUsers()
    const form = useForm({
        initialValues: {
            customer_id: String(customerId ?? ''),
            rw_user_id: rwUserId ? String(rwUserId) : null,
            tariff_id: null as string | null,
            label: '',
            price_override: '' as number | '',
            auto_extend: true
        },
        validate: { customer_id: (v) => (v ? null : t('sub.choose_customer')) }
    })
    const rwOptions = useMemo(
        () =>
            (rwUsers.data ?? [])
                .filter((u) => !u.linked || u.id === rwUserId)
                .map((u) => ({ value: String(u.id), label: rwLabel(u) })),
        [rwUsers.data, rwUserId]
    )
    const picked = rwUsers.data?.find((u) => String(u.id) === form.values.rw_user_id)
    const m = useApiMutation((v: typeof form.values) => {
        const body = {
            customer_id: Number(v.customer_id),
            rw_user_id: v.rw_user_id ? Number(v.rw_user_id) : null,
            tariff_id: v.tariff_id ? Number(v.tariff_id) : null,
            label: v.label,
            price_override: v.price_override === '' ? null : Number(v.price_override),
            auto_extend: v.auto_extend
        }
        return api.post('subscriptions', body)
    })
    return (
        <form
            onSubmit={form.onSubmit((v) =>
                m.mutate(v, {
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
                    <>
                        <FormSection color="blue" icon={TbUser} title={t('sub.customer')} description={t('sub.customer_hint')}>
                            <SearchSelect
                                data={(customers.data ?? []).map((c) => ({ value: String(c.id), label: c.name }))}
                                label={t('sub.customer')}
                                leftSection={<PiUser size={16} />}
                                required
                                {...form.getInputProps('customer_id')}
                            />
                            <TextInput
                                description={t('sub.label_hint')}
                                label={t('sub.label')}
                                leftSection={<PiTag size={16} />}
                                placeholder={t('sub.label_placeholder')}
                                {...form.getInputProps('label')}
                            />
                        </FormSection>
                        <FormSection color="brand" icon={PiUserCircleDuotone} title={t('sub.panel_user')} description={t('sub.panel_user_hint')}>
                            <RwUserSelect options={rwOptions} users={rwUsers.data} {...form.getInputProps('rw_user_id')} />
                            {picked && <RwUserPreview user={picked} />}
                        </FormSection>
                    </>
                }
                right={
                    <>
                        <FormSection color="teal" icon={PiTagDuotone} title={t('sub.tariff_price')} description={t('sub.tariff_price_hint')}>
                            <SearchSelect
                                allowDeselect
                                clearable
                                data={(tariffs.data ?? []).filter((t) => t.kind === 'base').map((t) => ({ value: String(t.id), label: tariffLabel(t) }))}
                                description={t('sub.tariff_no_rw')}
                                label={t('tariffs.tariff')}
                                leftSection={<PiTag size={16} />}
                                placeholder={t('sub.no_tariff')}
                                {...form.getInputProps('tariff_id')}
                            />
                            <NumberInput
                                decimalScale={2}
                                description={t('sub.price_override_hint')}
                                label={t('sub.price_override', { currency: currencySymbol() })}
                                leftSection={<CurrencyIcon size={16} />}
                                min={0}
                                {...form.getInputProps('price_override')}
                            />
                        </FormSection>
                        <FormSection color="orange" icon={PiArrowsClockwiseDuotone} title={t('sub.renewal')}>
                            <Switch
                                description={t('sub.auto_extend_hint')}
                                label={t('sub.auto_extend')}
                                thumbIcon={<PiArrowsClockwise size={10} />}
                                {...form.getInputProps('auto_extend', { type: 'checkbox' })}
                            />
                        </FormSection>
                    </>
                }
            />
            <FormFooter loading={m.isPending} onCancel={onDone} submitIcon={<PiLinkDuotone size={16} />} submitLabel={t('sub.link')} />
        </form>
    )
}

// openProvisionModal creates a brand-new panel user for a customer.
export function openProvisionModal(customer: { id: number; name: string }) {
    openModal({ icon: PiPlusCircleDuotone, color: 'teal', title: i18n.t('sub.new_title'), subtitle: customer.name }, (close) => <ProvisionForm customerId={customer.id} onDone={close} />, '1000px')
}

function ProvisionForm({ customerId, onDone }: { customerId: number; onDone: () => void }) {
    const { t } = useTranslation()
    const tariffs = useTariffs()
    const customers = useCustomers()
    const [tariffId, setTariffId] = useState<string | null>(null)
    const [username, setUsername] = useState('')
    const [label, setLabel] = useState('')
    const [term, setTerm] = useState<Term>({ months: 1, days: 0, until: null })
    const [amount, setAmount] = useState(0)
    const [allowDebt, setAllowDebt] = useState(false)
    const [picked, setPicked] = useState<Source | null>(null)
    const [income, setIncome] = useState<Income>(newIncome)
    const [incomeTouched, setIncomeTouched] = useState(false)
    const eligible = (tariffs.data ?? []).filter((t) => t.kind === 'base' && t.manage_rw && t.active)
    const tariff = eligible.find((t) => String(t.id) === tariffId)
    const balance = customers.data?.find((c) => c.id === customerId)?.balance
    // Unless picked by hand: the balance pays if it covers the term.
    const source: Source = picked ?? (balance !== undefined && amount > 0 && balance >= amount ? 'balance' : 'paid')
    const paid = source === 'paid'
    useEffect(() => {
        if (tariff) setAmount(periodCost(tariff.monthly_price, tariff.periods, term.months, term.days))
    }, [tariff, term])
    useEffect(() => {
        if (!incomeTouched) setIncome((v) => ({ ...v, amount: incomeFor(amount, balance ?? 0) }))
    }, [amount, balance, incomeTouched])
    // A payment goes on the balance first, then the subscription is bought
    // from it; if the panel refuses, the money stays on the balance.
    const m = useApiMutation(async () => {
        if (paid) {
            await api.post(`customers/${customerId}/payments`, { amount: income.amount, date: income.date, method: income.method ?? '', note: '', allocations: [] })
        }
        const body = {
            customer_id: customerId,
            tariff_id: Number(tariffId),
            username,
            label,
            months: term.months,
            days: term.days,
            amount,
            allow_debt: paid || allowDebt
        }
        try {
            return await api.post('subscriptions/provision', body)
        } catch (e) {
            throw paid ? new Error(t('sub.paid_not_created', { amount: fmtMoney(income.amount, 2), error: errorText(e) })) : e
        }
    })
    return (
        <>
            {eligible.length === 0 && (
                <Alert color="yellow" mb="md" variant="soft">
                    {t('sub.no_managed_tariffs')}
                </Alert>
            )}
            <FormColumns
                left={
                    <>
                        <FormSection color="brand" icon={PiUserCircleDuotone} title={t('sub.panel_user_new')} description={t('sub.panel_user_new_hint')}>
                            <Select
                                data={eligible.map((t) => ({ value: String(t.id), label: tariffLabel(t) }))}
                                label={t('tariffs.tariff')}
                                leftSection={<PiTag size={16} />}
                                onChange={setTariffId}
                                placeholder={t('sub.choose_tariff')}
                                value={tariffId}
                            />
                            <TextInput
                                data-autofocus
                                description={t('sub.username_hint')}
                                label="Username"
                                leftSection={<PiUser size={16} />}
                                onChange={(e) => setUsername(e.currentTarget.value.trim())}
                                value={username}
                            />
                            <TextInput
                                label={t('sub.label')}
                                leftSection={<PiTag size={16} />}
                                onChange={(e) => setLabel(e.currentTarget.value)}
                                placeholder={t('sub.label_placeholder')}
                                value={label}
                            />
                        </FormSection>
                        <TermSection
                            description={t('sub.from_today')}
                            from={null}
                            onChange={setTerm}
                            presets={tariff ? tariffPresets(tariff.monthly_price, tariff.periods) : []}
                            value={term}
                        />
                    </>
                }
                right={
                    <>
                        <SourceSwitch onChange={setPicked} value={source} />
                        {paid && (
                            <IncomeSection
                                onChange={(v) => {
                                    setIncomeTouched(true)
                                    setIncome(v)
                                }}
                                value={income}
                            />
                        )}
                        <PaymentSection
                            allowDebt={allowDebt}
                            amount={amount}
                            balance={balance}
                            income={paid ? income.amount : undefined}
                            monthly={tariff?.monthly_price}
                            onAllowDebt={setAllowDebt}
                            onAmount={setAmount}
                        />
                    </>
                }
            />
            <FormFooter
                disabled={!tariffId || username.length < 3 || term.months + term.days <= 0 || (paid && (income.amount <= 0 || !income.date))}
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    m.mutate(undefined, {
                        onSuccess: () => {
                            notifyOk(t('sub.created'))
                            onDone()
                        },
                        onError: (e) => notifyError(e)
                    })
                }
                submitIcon={<PiPlusCircle size={16} />}
                submitLabel={t('sub.create_in_panel')}
            />
        </>
    )
}

export function openConnectAddonModal(sub: Subscription) {
    openModal({ icon: PiPuzzlePieceDuotone, color: 'grape', title: i18n.t('sub.connect_addon'), subtitle: sub.title }, (close) => <ConnectAddonForm sub={sub} onDone={close} />, '1000px')
}

function ConnectAddonForm({ sub, onDone }: { sub: Subscription; onDone: () => void }) {
    const { t } = useTranslation()
    const tariffs = useTariffs()
    const customers = useCustomers()
    const connected = new Set(sub.addons.map((a) => a.addon_id))
    const options = (tariffs.data ?? []).filter((t) => t.kind === 'addon' && t.active && !connected.has(t.addon_id ?? 0))
    const groups = Object.entries(
        options.reduce<Record<string, { value: string; label: string }[]>>((acc, t) => {
            ;(acc[t.addon_name] ??= []).push({ value: String(t.id), label: tariffLabel(t) })
            return acc
        }, {})
    ).map(([group, items]) => ({ group, items }))
    // By default the add-on runs until the subscription is paid up, so
    // connecting it mid-period charges only the rest of that period.
    // An unlimited subscription gets an unlimited, free add-on.
    const subEnd = sub.rw_user?.expire_at && dayjs(sub.rw_user.expire_at).isAfter(dayjs().add(1, 'hour')) ? sub.rw_user.expire_at : null
    const subUnlimited = isUnlimited(subEnd)
    const alignTerm: Term | null = useMemo(
        () => (!subEnd ? null : subUnlimited ? { months: 0, days: 0, until: subEnd } : { ...termBetween(dayjs(), dayjs(subEnd)), until: subEnd }),
        [subEnd, subUnlimited]
    )
    const [tariffId, setTariffId] = useState<string | null>(null)
    const [term, setTerm] = useState<Term>(alignTerm ?? { months: 1, days: 0, until: null })
    const [amount, setAmount] = useState(0)
    const [allowDebt, setAllowDebt] = useState(false)
    const tariff = options.find((t) => String(t.id) === tariffId)
    useEffect(() => {
        if (tariff) setAmount(periodCost(tariff.monthly_price, tariff.periods, term.months, term.days))
    }, [tariff, term])
    const presets: TermPreset[] = [
        ...(alignTerm
            ? [
                  {
                      label: subUnlimited ? t('sub.forever_like_sub') : t('sub.until_sub_end', { date: fmtDate(subEnd) }),
                      hint: tariff ? fmtMoney(periodCost(tariff.monthly_price, tariff.periods, alignTerm.months, alignTerm.days)) : undefined,
                      term: alignTerm
                  }
              ]
            : []),
        ...(tariff ? tariffPresets(tariff.monthly_price, tariff.periods) : [])
    ]
    const m = useApiMutation(() =>
        api.post(`subscriptions/${sub.id}/addons`, {
            tariff_id: Number(tariffId),
            months: term.months,
            days: term.days,
            until: term.until,
            amount,
            allow_debt: allowDebt
        })
    )
    return (
        <>
            {(!sub.rw_user || groups.length === 0) && (
                <Stack gap="xs" mb="md">
                    {!sub.rw_user && (
                        <Alert color="yellow" variant="soft">
                            {t('sub.link_first')}
                        </Alert>
                    )}
                    {groups.length === 0 && (
                        <Alert color="yellow" variant="soft">
                            {t('sub.no_addon_tariffs')}
                        </Alert>
                    )}
                </Stack>
            )}
            <FormColumns
                left={
                    <>
                        <FormSection color="grape" icon={PiPuzzlePieceDuotone} title={t('tariffs.addon')} description={t('sub.addon_hint')}>
                            <Select
                                data={groups}
                                label={t('sub.addon_tariff')}
                                leftSection={<PiPuzzlePiece size={16} />}
                                onChange={setTariffId}
                                placeholder={t('errors.tariff.addon_required')}
                                value={tariffId}
                            />
                            {tariff && !tariff.squad_uuids.length && (
                                <Alert color="orange" variant="soft">
                                    {t('sub.addon_no_squads')}
                                </Alert>
                            )}
                        </FormSection>
                        <TermSection description={t('sub.from_today')} from={null} onChange={setTerm} presets={presets} value={term} />
                    </>
                }
                right={
                    <PaymentSection
                        allowDebt={allowDebt}
                        amount={amount}
                        balance={customers.data?.find((c) => c.id === sub.customer_id)?.balance}
                        description={t('sub.addon_amount_hint')}
                        monthly={tariff?.monthly_price}
                        onAllowDebt={setAllowDebt}
                        onAmount={setAmount}
                    />
                }
            />
            <FormFooter
                disabled={!tariffId || !sub.rw_user || (term.months + term.days <= 0 && !term.until)}
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    m.mutate(undefined, {
                        onSuccess: () => {
                            notifyOk(t('sub.addon_connected'))
                            onDone()
                        },
                        onError: (e) => notifyError(e)
                    })
                }
                submitIcon={<PiPuzzlePiece size={16} />}
                submitLabel={t('sub.connect')}
            />
        </>
    )
}

export function openChangeTariffModal(p: { kind: 'subscription' | 'addon'; id: number; title: string; tariffId: number | null; addonId?: number }) {
    openModal({ icon: PiArrowsLeftRightDuotone, color: 'indigo', title: i18n.t('sub.change_tariff'), subtitle: p.title }, (close) => <ChangeTariffForm {...p} onDone={close} />, '1000px')
}

function ChangeTariffForm({
    kind,
    id,
    tariffId,
    addonId,
    onDone
}: {
    kind: 'subscription' | 'addon'
    id: number
    tariffId: number | null
    addonId?: number
    onDone: () => void
}) {
    const { t } = useTranslation()
    const tariffs = useTariffs()
    const options = (tariffs.data ?? []).filter((t) =>
        kind === 'subscription' ? t.kind === 'base' : t.kind === 'addon' && t.addon_id === addonId
    )
    const [target, setTarget] = useState<string | null>(null)
    const [surcharge, setSurcharge] = useState(0)
    const [clearOverride, setClearOverride] = useState(true)
    // removed: subscription add-on id → 'disable' | 'keep_paid'
    const [removed, setRemoved] = useState<Record<number, string>>({})
    const quote = useQuery({
        queryKey: ['tariff-quote', kind, id, target],
        enabled: !!target,
        queryFn: () => api.get<TariffQuote>(`items/${kind}/${id}/tariff-quote?tariff_id=${target}`)
    })
    useEffect(() => {
        if (!quote.data) return
        // a downgrade isn't refunded, the unused paid time of add-ons that
        // become included is
        const credits = quote.data.addons.reduce((s, a) => s + a.credit, 0)
        setSurcharge(Math.max(0, quote.data.surcharge + credits) - credits)
        setRemoved({})
    }, [quote.data])
    const picked = options.find((x) => String(x.id) === target)
    const m = useApiMutation(() =>
        api.post(`items/${kind}/${id}/tariff`, {
            tariff_id: Number(target),
            surcharge,
            clear_override: clearOverride,
            removed_addons: Object.fromEntries(
                (quote.data?.addons ?? [])
                    .filter((a) => a.action === 'remove' && a.subscription_addon_id)
                    .map((a) => [a.subscription_addon_id, removed[a.subscription_addon_id!] ?? 'disable'])
            )
        })
    )
    return (
        <>
            <FormColumns
                left={
                    <FormSection color="indigo" icon={PiArrowsLeftRightDuotone} title={t('tariffs.new_tariff')} description={t('sub.compare_hint')}>
                        <Select
                            label={t('tariffs.tariff')}
                            leftSection={<PiTag size={16} />}
                            data={options.filter((o) => o.id !== tariffId).map((o) => ({ value: String(o.id), label: tariffLabel(o) }))}
                            placeholder={t('sub.choose_tariff')}
                            value={target}
                            onChange={setTarget}
                        />
                        {quote.data && (
                            <Paper bd="1px solid var(--app-border)" bg="var(--app-surface-2)" p="sm" radius="md">
                                <Stack gap={4}>
                                    <Group justify="space-between">
                                        <Text c="dimmed" size="sm">
                                            {t('sub.per_month')}
                                        </Text>
                                        <Text className="num" fw={600} size="sm">
                                            {fmtMoney(quote.data.old_monthly)} → {fmtMoney(quote.data.new_monthly)}
                                        </Text>
                                    </Group>
                                    <Group justify="space-between">
                                        <Text c="dimmed" size="sm">
                                            {t('sub.paid_until')}
                                        </Text>
                                        <Text className="num" size="sm">
                                            {fmtDate(quote.data.expire_at)}
                                        </Text>
                                    </Group>
                                    <Group justify="space-between">
                                        <Text c="dimmed" size="sm">
                                            {t('sub.prorated')}
                                        </Text>
                                        <Text className="num" fw={600} size="sm">
                                            {fmtMoney(quote.data.surcharge, 2)}
                                        </Text>
                                    </Group>
                                </Stack>
                            </Paper>
                        )}
                        {!!quote.data?.addons.length && (
                            <Stack gap="xs">
                                <Text fw={500} size="sm">
                                    {t('sub.tariff_addons')}
                                </Text>
                                {quote.data.addons.map((a) => (
                                    <Paper key={`${a.action}-${a.addon_id}`} p="xs" radius="md" withBorder>
                                        <Group gap="xs" justify="space-between" wrap="nowrap">
                                            <Stack gap={0} miw={0}>
                                                <Text fw={500} size="sm" truncate="end">
                                                    {a.addon_name}
                                                </Text>
                                                <Text c="dimmed" size="xs">
                                                    {a.action === 'connect' && t('sub.addon_will_connect', { tariff: a.tariff_name })}
                                                    {a.action === 'include' &&
                                                        (a.credit > 0 ? t('sub.addon_will_include_credit', { credit: fmtMoney(a.credit, 2) }) : t('sub.addon_will_include'))}
                                                    {a.action === 'remove' && t('sub.addon_removed')}
                                                </Text>
                                            </Stack>
                                            {a.action === 'remove' ? (
                                                <SegmentedControl
                                                    size="xs"
                                                    data={[
                                                        { value: 'disable', label: t('sub.disable') },
                                                        { value: 'keep_paid', label: t('sub.keep_paid') }
                                                    ]}
                                                    value={removed[a.subscription_addon_id!] ?? 'disable'}
                                                    onChange={(v) => setRemoved((r) => ({ ...r, [a.subscription_addon_id!]: v }))}
                                                />
                                            ) : (
                                                <Badge color={a.action === 'connect' ? 'teal' : 'grape'} variant="soft">
                                                    {a.action === 'connect' ? t('sub.badge_new') : t('sub.badge_include')}
                                                </Badge>
                                            )}
                                        </Group>
                                    </Paper>
                                ))}
                            </Stack>
                        )}
                        {picked && (
                            <Text c="dimmed" size="xs">
                                {picked.manage_rw ? t('sub.rw_will_update') : t('sub.rw_unchanged')}
                            </Text>
                        )}
                    </FormSection>
                }
                right={
                    <FormSection icon={PiWalletDuotone} color="orange" title={t('sub.surcharge_title')} description={t('sub.surcharge_hint')}>
                        <NumberInput
                            label={t('sub.surcharge', { currency: currencySymbol() })}
                            description={t('sub.surcharge_desc')}
                            leftSection={<CurrencyIcon size={16} />}
                            decimalScale={2}
                            value={surcharge}
                            onChange={(v) => setSurcharge(Number(v) || 0)}
                        />
                        <Switch
                            label={t('sub.clear_override')}
                            description={t('sub.clear_override_hint')}
                            checked={clearOverride}
                            onChange={(e) => setClearOverride(e.currentTarget.checked)}
                        />
                    </FormSection>
                }
            />
            <FormFooter
                disabled={!target}
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    m.mutate(undefined, {
                        onSuccess: () => {
                            notifyOk(t('sub.tariff_changed'))
                            onDone()
                        },
                        onError: (e) => notifyError(e)
                    })
                }
                submitIcon={<PiArrowsLeftRight size={16} />}
                submitLabel={t('sub.change')}
            />
        </>
    )
}

export function openTrafficAnchorModal(p: { kind: 'subscription' | 'addon'; id: number; title: string; anchor: string }) {
    openModal({ icon: PiCalendarDuotone, color: 'indigo', title: i18n.t('sub.traffic_anchor_edit'), subtitle: p.title }, (close) => <TrafficAnchorForm {...p} onDone={close} />)
}

function TrafficAnchorForm({ kind, id, anchor, onDone }: { kind: 'subscription' | 'addon'; id: number; anchor: string; onDone: () => void }) {
    const { t } = useTranslation()
    const [value, setValue] = useState<string | null>(dayjs(anchor).format('YYYY-MM-DD HH:mm:ss'))
    const m = useApiMutation(() =>
        api.put(kind === 'subscription' ? `subscriptions/${id}/traffic-anchor` : `subscription-addons/${id}/traffic-anchor`, { anchor: dayjs(value).toISOString() })
    )
    return (
        <>
            <DateTimePicker label={t('sub.traffic_anchor')} description={t('sub.traffic_anchor_hint')} leftSection={<PiCalendarDuotone size={16} />} value={value} onChange={setValue} />
            <FormFooter
                disabled={!value}
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    m.mutate(undefined, {
                        onSuccess: () => {
                            notifyOk(t('common.saved'))
                            onDone()
                        },
                        onError: (e) => notifyError(e)
                    })
                }
                submitIcon={<PiCalendarDuotone size={16} />}
                submitLabel={t('common.save')}
            />
        </>
    )
}
