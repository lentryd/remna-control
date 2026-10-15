import { ActionIcon, Alert, Box, Button, Group, Menu, NumberInput, Stack, Switch, Text, TextInput, Tooltip, UnstyledButton } from '@mantine/core'
import { useForm } from '@mantine/form'
import dayjs from 'dayjs'
import { type ReactNode, useEffect, useMemo, useRef } from 'react'
import {
    PiArrowSquareOut,
    PiArrowsLeftRight,
    PiArrowsClockwise,
    PiFloppyDiskDuotone,
    PiTag,
    PiTagDuotone,
    PiUser,
    PiCalendarPlus,
    PiCopy,
    PiHexagonDuotone,
    PiPuzzlePiece,
    PiPuzzlePieceDuotone,
    PiLinkBreak
} from 'react-icons/pi'
import { TbDots, TbUser } from 'react-icons/tb'
import { Link } from 'react-router'

import { api } from '@/api/client'
import { useApiMutation, useCustomers, useRwUsers, useSubscriptions, useTariffs } from '@/api/hooks'
import type { AddonItem, RwUser, Subscription } from '@/api/types'
import { Dot, ExpireCell, lastSeenColor, OnlineCell, StatusBadge, TrafficCell } from '@/components/badges'
import { fmtDate, fmtMoney, currencySymbol } from '@/components/format'
import { notifyError, notifyOk } from '@/components/notify'
import { addonTitle, AddonMenuItems, SubscriptionMenuItems, useItemActions } from '@/components/ItemActions'
import { Money } from '@/components/ui'
import { openExtendModal } from '@/modals/ExtendModal'
import { openChangeTariffModal, openConnectAddonModal, RwUserSelect, rwLabel, tariffLabel } from '@/modals/SubscriptionModals'
import { FormColumns, FormSection } from '@shared/ui/forms/form-section'
import { LoadingScreen } from '@shared/ui/loading-screen'
import { ModalFooter } from '@shared/ui/modal-footer'

import { openModal } from './open'
import classes from './view-item.module.css'
import { SearchSelect } from '@shared/ui/forms/search-select'
import { CurrencyIcon } from '@shared/currencies'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

// IdentityPanel is the panel user behind a subscription or add-on: who it
// is, its status and links, then expiry, traffic and last connection.
function IdentityPanel({ rw, caption }: { rw: RwUser | null; caption?: string }) {
    const { t } = useTranslation()
    const { copyLink } = useItemActions()
    if (!rw) {
        return (
            <Alert color="yellow" icon={<PiLinkBreak size={18} />} mb="md" title={caption}>
                {t('view.not_linked')}
            </Alert>
        )
    }
    const online = !!rw.online_at && dayjs().diff(dayjs(rw.online_at), 'minute') <= 5
    return (
        <div className={classes.identity}>
            <div className={classes.identityHead}>
                <Group gap="sm" miw={0} wrap="nowrap">
                    <Dot color={rw.online_at ? lastSeenColor(rw.online_at) : 'var(--app-border-strong)'} pulse={online} size={9} />
                    <Box miw={0}>
                        {caption && (
                            <Text c="dimmed" size="xs">
                                {caption}
                            </Text>
                        )}
                        <Text c="var(--app-text-strong)" fw={600} truncate="end">
                            {rw.username}
                        </Text>
                        <Text c="dimmed" className="num" size="xs">
                            ID {rw.id}
                            {rw.short_uuid ? ` · ${rw.short_uuid}` : ''}
                        </Text>
                    </Box>
                </Group>
                <Group gap={6} wrap="nowrap">
                    <StatusBadge user={rw} />
                    <Tooltip label={t('view.copy_link')}>
                        <ActionIcon onClick={() => copyLink(rw.subscription_url)} size="lg" variant="default">
                            <PiCopy size={16} />
                        </ActionIcon>
                    </Tooltip>
                    {rw.subscription_url && (
                        <Tooltip label={t('view.open_page')}>
                            <ActionIcon component="a" href={rw.subscription_url} rel="noopener noreferrer" size="lg" target="_blank" variant="default">
                                <PiArrowSquareOut size={16} />
                            </ActionIcon>
                        </Tooltip>
                    )}
                </Group>
            </div>
            <div className={classes.identityFacts}>
                <div className={classes.fact}>
                    <Text className={classes.factLabel}>{t('sub.paid_until')}</Text>
                    <ExpireCell align="flex-start" date={rw.expire_at} />
                </div>
                <div className={classes.fact} data-wide>
                    <Text className={classes.factLabel}>{t('sub.traffic')}</Text>
                    <TrafficCell user={rw} />
                </div>
                <div className={classes.fact}>
                    <Text className={classes.factLabel}>{t('card.last_online')}</Text>
                    <OnlineCell user={rw} />
                </div>
            </div>
        </div>
    )
}

type SubValues = {
    customer_id: string
    rw_user_id: string | null
    label: string
    tariff_id: string | null
    price_override: number | ''
    auto_extend: boolean
}

const subValues = (sub: Subscription): SubValues => ({
    customer_id: String(sub.customer_id),
    rw_user_id: sub.rw_user ? String(sub.rw_user.id) : null,
    label: sub.label,
    tariff_id: sub.tariff_id ? String(sub.tariff_id) : null,
    price_override: sub.price_override ?? '',
    auto_extend: sub.auto_extend
})

type AddonValues = { price_override: number | ''; auto_extend: boolean }

const addonValues = (a: AddonItem): AddonValues => ({ price_override: a.price_override ?? '', auto_extend: a.auto_extend })

// useSyncedForm is a form over server data that also changes elsewhere
// (another modal, a refetch): fields the user hasn't touched follow the
// server, edited ones are kept.
function useSyncedForm<T extends Record<string, unknown>>(server: T) {
    const form = useForm<T>({ initialValues: server })
    const base = useRef(server)
    const key = JSON.stringify(server)
    useEffect(() => {
        const prev = base.current
        if (JSON.stringify(prev) === key) return
        const next = { ...form.getValues() }
        for (const k of Object.keys(server) as (keyof T)[]) {
            if (next[k] === prev[k]) next[k] = server[k]
        }
        base.current = server
        form.setInitialValues(server)
        form.setValues(next)
        form.resetDirty(server)
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [key])
    return form
}

function CustomerLink({ id, onNavigate }: { id: number; onNavigate: () => void }) {
    const { t } = useTranslation()
    return (
        <Button component={Link} onClick={onNavigate} rightSection={<PiArrowSquareOut size={14} />} size="xs" to={`/customers/${id}`} variant="default">
            {t('sub.customer')}
        </Button>
    )
}

// MiniIdentity is a one-line summary of a panel user.
function MiniIdentity({ rw }: { rw: RwUser | null }) {
    const { t } = useTranslation()
    if (!rw) return <Text c="dimmed" size="sm">{t('view.not_linked')}</Text>
    return (
        <Stack gap="sm">
            <Group justify="space-between" wrap="nowrap">
                <Text fw={500} size="sm" truncate="end">
                    {rw.username}
                </Text>
                <StatusBadge user={rw} />
            </Group>
            <ExpireCell align="flex-start" date={rw.expire_at} />
            <TrafficCell user={rw} />
        </Stack>
    )
}

function SwitchAutoExtend(props: ReturnType<ReturnType<typeof useForm>['getInputProps']>) {
    const { t } = useTranslation()
    return (
        <Switch
            description={t('sub.auto_extend_hint')}
            label={t('sub.auto_extend')}
            thumbIcon={<PiArrowsClockwise size={10} />}
            {...props}
        />
    )
}

function AddonsCard({ sub }: { sub: Subscription }) {
    const { t } = useTranslation()
    return (
        <FormSection
            actions={
                <Button disabled={!sub.rw_user} leftSection={<PiPuzzlePiece size={15} />} onClick={() => openConnectAddonModal(sub)} size="xs" variant="default">
                    {t('sub.connect')}
                </Button>
            }
            color="grape"
            icon={PiPuzzlePieceDuotone}
            title={t('view.addons')}
        >
            {sub.addons.length === 0 && (
                <Text c="dimmed" size="sm">
                    {t('view.no_addons')}
                </Text>
            )}
            {sub.addons.length > 0 && (
                <Stack gap={0} className={classes.addonList}>
                    {sub.addons.map((a) => (
                        <UnstyledButton className={classes.addonRow} key={a.id} onClick={() => openViewAddonModal(a.id, sub.id)}>
                            <Box miw={0}>
                                <Text fw={500} size="sm" truncate="end">
                                    {a.addon_name}
                                    <Text c="dimmed" component="span" fw={400} inherit>
                                        {' · '}
                                        {a.tariff_name || t('view.no_tariff')}
                                    </Text>
                                </Text>
                                <Text c="dimmed" size="xs">
                                    {a.rw_user?.unlimited ? t('expiry.forever') : t('view.until', { date: fmtDate(a.rw_user?.expire_at) })} · <Money value={a.price} />
                                </Text>
                            </Box>
                            <StatusBadge user={a.rw_user} />
                        </UnstyledButton>
                    ))}
                </Stack>
            )}
        </FormSection>
    )
}

function MoreMenu({ children }: { children: ReactNode }) {
    const { t } = useTranslation()
    return (
        <Menu keepMounted position="top-end" shadow="md">
            <Menu.Target>
                <Button leftSection={<TbDots size={18} />} mr="auto" variant="default">
                    {t('customer.more')}
                </Button>
            </Menu.Target>
            <Menu.Dropdown>{children}</Menu.Dropdown>
        </Menu>
    )
}

function ViewSubscription({ id, close }: { id: number; close: () => void }) {
    const { t } = useTranslation()
    const { data } = useSubscriptions()
    const sub = data?.find((s) => s.id === id)
    if (!data) return <LoadingScreen height="40vh" />
    if (!sub)
        return (
            <Text c="dimmed" p="md">
                {t('view.sub_deleted')}
            </Text>
        )
    return <SubscriptionEditor close={close} sub={sub} />
}

// SubscriptionEditor is the subscription modal: like the panel's user
// modal, the cards are the edit form and the footer saves them.
function SubscriptionEditor({ sub, close }: { sub: Subscription; close: () => void }) {
    const { t } = useTranslation()
    const customers = useCustomers()
    const tariffs = useTariffs()
    const rwUsers = useRwUsers()
    const form = useSyncedForm(subValues(sub))
    const rwOptions = useMemo(
        () => (rwUsers.data ?? []).filter((u) => !u.linked || u.id === sub.rw_user?.id).map((u) => ({ value: String(u.id), label: rwLabel(u) })),
        [rwUsers.data, sub.rw_user?.id]
    )
    const tariff = tariffs.data?.find((t) => String(t.id) === form.values.tariff_id)
    const m = useApiMutation((v: SubValues) =>
        api.put(`subscriptions/${sub.id}`, {
            customer_id: Number(v.customer_id),
            rw_user_id: v.rw_user_id ? Number(v.rw_user_id) : null,
            tariff_id: v.tariff_id ? Number(v.tariff_id) : null,
            label: v.label,
            price_override: v.price_override === '' ? null : Number(v.price_override),
            auto_extend: v.auto_extend
        })
    )
    const save = form.onSubmit((v) =>
        m.mutate(v, {
            onSuccess: () => {
                form.resetDirty(v)
                notifyOk(t('common.saved'))
            },
            onError: (e) => notifyError(e)
        })
    )

    return (
        <form className="remnac-fade" onSubmit={save}>
            <IdentityPanel rw={sub.rw_user} />
            <FormColumns
                left={
                    <>
                        <FormSection
                            actions={<CustomerLink id={sub.customer_id} onNavigate={close} />}
                            color="indigo"
                            description={t('view.records_hint')}
                            icon={TbUser}
                            title={t('view.records')}
                        >
                            <SearchSelect
                                data={(customers.data ?? []).map((c) => ({ value: String(c.id), label: c.name }))}
                                label={t('sub.customer')}
                                leftSection={<PiUser size={16} />}
                                {...form.getInputProps('customer_id')}
                            />
                            <TextInput
                                description={t('sub.label_hint')}
                                label={t('sub.label')}
                                leftSection={<PiTag size={16} />}
                                placeholder={t('sub.label_placeholder')}
                                {...form.getInputProps('label')}
                            />
                            <RwUserSelect
                                description={t('sub.panel_user_hint')}
                                options={rwOptions}
                                users={rwUsers.data}
                                {...form.getInputProps('rw_user_id')}
                            />
                        </FormSection>
                    </>
                }
                right={
                    <>
                        <FormSection color="teal" description={t('sub.tariff_price_hint')} icon={PiTagDuotone} title={t('sub.tariff_price')}>
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
                                description={tariff ? t('view.price_override_hint', { price: fmtMoney(tariff.monthly_price) }) : t('sub.price_override_hint')}
                                label={t('sub.price_override', { currency: currencySymbol() })}
                                leftSection={<CurrencyIcon size={16} />}
                                min={0}
                                {...form.getInputProps('price_override')}
                            />
                            <SwitchAutoExtend {...form.getInputProps('auto_extend', { type: 'checkbox' })} />
                        </FormSection>
                        <AddonsCard sub={sub} />
                    </>
                }
            />
            <ModalFooter isMobile={window.matchMedia('(max-width: 40em)').matches}>
                <MoreMenu>
                    <SubscriptionMenuItems inView onUnlinked={close} sub={sub} />
                </MoreMenu>
                <Button
                    leftSection={<PiArrowsLeftRight size={16} />}
                    onClick={() => openChangeTariffModal({ kind: 'subscription', id: sub.id, title: sub.title, tariffId: sub.tariff_id })}
                    variant="default"
                >
                    {t('sub.change_tariff')}
                </Button>
                <Button
                    disabled={!sub.rw_user || sub.rw_user.unlimited}
                    leftSection={<PiCalendarPlus size={16} />}
                    onClick={() => openExtendModal({ kind: 'subscription', id: sub.id, title: sub.title })}
                    variant={form.isDirty() ? 'default' : 'filled'}
                >
                    {t('dashboard.extend')}
                </Button>
                <SaveButton dirty={form.isDirty()} loading={m.isPending} />
            </ModalFooter>
        </form>
    )
}

function SaveButton({ dirty, loading }: { dirty: boolean; loading: boolean }) {
    const { t } = useTranslation()
    return (
        <Button disabled={!dirty} leftSection={<PiFloppyDiskDuotone size={16} />} loading={loading} type="submit" variant={dirty ? 'filled' : 'default'}>
            {t('common.save')}
        </Button>
    )
}

function ViewAddon({ id, subId, close }: { id: number; subId: number; close: () => void }) {
    const { t } = useTranslation()
    const { data } = useSubscriptions()
    const sub = data?.find((s) => s.id === subId)
    const addon: AddonItem | undefined = sub?.addons.find((a) => a.id === id)
    if (!data) return <LoadingScreen height="40vh" />
    if (!sub || !addon)
        return (
            <Text c="dimmed" p="md">
                {t('view.addon_deleted')}
            </Text>
        )
    return <AddonEditor addon={addon} close={close} sub={sub} />
}

function AddonEditor({ addon, sub, close }: { addon: AddonItem; sub: Subscription; close: () => void }) {
    const { t } = useTranslation()
    const tariffs = useTariffs()
    const form = useSyncedForm(addonValues(addon))
    const title = addonTitle(addon, sub.title)
    const tariff = tariffs.data?.find((t) => t.id === addon.tariff_id)
    const m = useApiMutation((v: AddonValues) =>
        api.put(`subscription-addons/${addon.id}`, {
            price_override: v.price_override === '' ? null : Number(v.price_override),
            auto_extend: v.auto_extend
        })
    )
    const save = form.onSubmit((v) =>
        m.mutate(v, {
            onSuccess: () => {
                form.resetDirty(v)
                notifyOk(t('common.saved'))
            },
            onError: (e) => notifyError(e)
        })
    )

    return (
        <form className="remnac-fade" onSubmit={save}>
            <IdentityPanel rw={addon.rw_user} />
            <FormColumns
                left={
                    <>
                        <FormSection
                            actions={<CustomerLink id={sub.customer_id} onNavigate={close} />}
                            color="grape"
                            description={`${sub.customer_name} · ${addon.tariff_name || t('view.no_tariff')}`}
                            icon={PiTagDuotone}
                            title={t('view.price_renewal')}
                        >
                            {addon.included ? (
                                <Text c="dimmed" size="sm">
                                    {t('view.included_hint')}
                                </Text>
                            ) : (
                                <>
                                    <NumberInput
                                        decimalScale={2}
                                        description={tariff ? t('view.price_override_hint', { price: fmtMoney(tariff.monthly_price) }) : t('sub.price_override_hint')}
                                        label={t('sub.price_override', { currency: currencySymbol() })}
                                        leftSection={<CurrencyIcon size={16} />}
                                        min={0}
                                        {...form.getInputProps('price_override')}
                                    />
                                    <SwitchAutoExtend {...form.getInputProps('auto_extend', { type: 'checkbox' })} />
                                </>
                            )}
                        </FormSection>
                    </>
                }
                right={
                    <FormSection color="brand" icon={PiHexagonDuotone} title={t('view.main_subscription', { title: sub.title })}>
                        <MiniIdentity rw={sub.rw_user} />
                    </FormSection>
                }
            />
            <ModalFooter isMobile={window.matchMedia('(max-width: 40em)').matches}>
                <MoreMenu>
                    <AddonMenuItems addon={addon} inView onUnlinked={close} subTitle={sub.title} />
                </MoreMenu>
                {!addon.included && (
                    <Button
                        leftSection={<PiArrowsLeftRight size={16} />}
                        onClick={() =>
                            openChangeTariffModal({ kind: 'addon', id: addon.id, title, tariffId: addon.tariff_id, addonId: addon.addon_id })
                        }
                        variant="default"
                    >
                        {t('sub.change_tariff')}
                    </Button>
                )}
                <Button
                    disabled={!addon.rw_user || addon.rw_user.unlimited || addon.included}
                    leftSection={<PiCalendarPlus size={16} />}
                    onClick={() => openExtendModal({ kind: 'addon', id: addon.id, title })}
                    variant={form.isDirty() ? 'default' : 'filled'}
                >
                    {t('dashboard.extend')}
                </Button>
                <SaveButton dirty={form.isDirty()} loading={m.isPending} />
            </ModalFooter>
        </form>
    )
}

export function openViewSubscriptionModal(sub: Pick<Subscription, 'id' | 'title' | 'customer_name'>) {
    openModal(
        { icon: PiHexagonDuotone, title: sub.title, subtitle: sub.customer_name },
        (close) => <ViewSubscription close={close} id={sub.id} />,
        '1000px'
    )
}

export function openViewAddonModal(addonId: number, subId: number) {
    openModal(
        { icon: PiPuzzlePieceDuotone, color: 'grape', title: i18n.t('tariffs.addon') },
        (close) => <ViewAddon close={close} id={addonId} subId={subId} />,
        '1000px'
    )
}
