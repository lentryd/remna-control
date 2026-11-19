import { ActionIcon, Menu } from '@mantine/core'
import { useClipboard } from '@mantine/hooks'
import { IconArrowsLeftRight, IconCalendarPlus, IconCopy, IconDots, IconLinkOff, IconPencil, IconPlayerPause, IconPlayerPlay, IconPuzzle } from '@tabler/icons-react'

import { api } from '@/api/client'
import { useInvalidateAll } from '@/api/hooks'
import type { AddonItem, Subscription } from '@/api/types'
import { openExtendModal } from '@/modals/ExtendModal'
import { confirmDanger } from '@/modals/open'
import { openChangeTariffModal, openConnectAddonModal } from '@/modals/SubscriptionModals'
import { openViewAddonModal, openViewSubscriptionModal } from '@/modals/ViewItemModal'

import { notifyError, notifyOk } from './notify'
import { useTranslation } from 'react-i18next'

export const addonTitle = (addon: AddonItem, subTitle: string) => `${addon.addon_name} · ${subTitle}`

export function useItemActions() {
    const { t } = useTranslation()
    const invalidate = useInvalidateAll()
    const clipboard = useClipboard()

    const toggle = async (kind: 'subscription' | 'addon', id: number, enable: boolean) => {
        try {
            await api.post(`items/${kind}/${id}/${enable ? 'enable' : 'disable'}`)
            await invalidate()
            notifyOk(enable ? t('actions.enabled') : t('actions.disabled'))
        } catch (e) {
            notifyError(e)
        }
    }

    const copyLink = (url: string) => {
        clipboard.copy(url)
        notifyOk(t('actions.link_copied'))
    }

    const unlinkSubscription = (sub: Subscription, onDone?: () => void) =>
        confirmDanger(
            t('actions.unlink_sub'),
            t('actions.unlink_sub_hint'),
            async () => {
                try {
                    await api.del(`subscriptions/${sub.id}`)
                    await invalidate()
                    onDone?.()
                } catch (e) {
                    notifyError(e)
                }
            },
            t('actions.unlink')
        )

    const unlinkAddon = (addon: AddonItem, onDone?: () => void) =>
        confirmDanger(
            t('actions.unlink_addon'),
            t('actions.unlink_addon_hint'),
            async () => {
                try {
                    await api.del(`subscription-addons/${addon.id}`)
                    await invalidate()
                    onDone?.()
                } catch (e) {
                    notifyError(e)
                }
            },
            t('actions.unlink')
        )

    return { toggle, copyLink, unlinkSubscription, unlinkAddon }
}

// SubscriptionMenuItems is the "more actions" list shared by row menus and
// the subscription modal footer (inView: the modal itself is the editor).
export function SubscriptionMenuItems({ sub, onUnlinked, inView }: { sub: Subscription; onUnlinked?: () => void; inView?: boolean }) {
    const { t } = useTranslation()
    const { toggle, copyLink, unlinkSubscription } = useItemActions()
    const disabled = sub.rw_user?.status === 'DISABLED'
    return (
        <>
            <Menu.Label>{t('expense_items.manage')}</Menu.Label>
            <Menu.Item
                leftSection={<IconCalendarPlus size={16} />}
                disabled={!sub.rw_user || sub.rw_user.unlimited}
                onClick={() => openExtendModal({ kind: 'subscription', id: sub.id, title: sub.title })}
            >
                {t('dashboard.extend')}
            </Menu.Item>
            <Menu.Item leftSection={<IconPuzzle size={16} />} disabled={!sub.rw_user} onClick={() => openConnectAddonModal(sub)}>
                {t('sub.connect_addon')}
            </Menu.Item>
            <Menu.Item
                leftSection={<IconArrowsLeftRight size={16} />}
                onClick={() => openChangeTariffModal({ kind: 'subscription', id: sub.id, title: sub.title, tariffId: sub.tariff_id })}
            >
                {t('sub.change_tariff')}
            </Menu.Item>
            {!inView && (
                <Menu.Item leftSection={<IconPencil size={16} />} onClick={() => openViewSubscriptionModal(sub)}>
                    {t('common.edit')}
                </Menu.Item>
            )}
            {sub.rw_user && (
                <>
                    <Menu.Item leftSection={<IconCopy size={16} />} onClick={() => copyLink(sub.rw_user!.subscription_url)}>
                        {t('view.copy_link')}
                    </Menu.Item>
                    <Menu.Item
                        leftSection={disabled ? <IconPlayerPlay size={16} /> : <IconPlayerPause size={16} />}
                        onClick={() => toggle('subscription', sub.id, disabled)}
                    >
                        {disabled ? t('actions.enable') : t('actions.disable')}
                    </Menu.Item>
                </>
            )}
            <Menu.Divider />
            <Menu.Label>{t('common.danger_zone')}</Menu.Label>
            <Menu.Item color="red" leftSection={<IconLinkOff size={16} />} onClick={() => unlinkSubscription(sub, onUnlinked)}>
                {t('actions.unlink')}
            </Menu.Item>
        </>
    )
}

export function AddonMenuItems({
    addon,
    subTitle,
    onUnlinked,
    inView
}: {
    addon: AddonItem
    subTitle: string
    onUnlinked?: () => void
    inView?: boolean
}) {
    const { t } = useTranslation()
    const { toggle, unlinkAddon } = useItemActions()
    const title = addonTitle(addon, subTitle)
    const disabled = addon.rw_user?.status === 'DISABLED'
    return (
        <>
            <Menu.Label>{addon.included ? t('actions.included') : t('expense_items.manage')}</Menu.Label>
            {!addon.included && (
                <>
                    <Menu.Item
                        leftSection={<IconCalendarPlus size={16} />}
                        disabled={!addon.rw_user || addon.rw_user.unlimited}
                        onClick={() => openExtendModal({ kind: 'addon', id: addon.id, title })}
                    >
                        {t('dashboard.extend')}
                    </Menu.Item>
                    <Menu.Item
                        leftSection={<IconArrowsLeftRight size={16} />}
                        onClick={() =>
                            openChangeTariffModal({ kind: 'addon', id: addon.id, title, tariffId: addon.tariff_id, addonId: addon.addon_id })
                        }
                    >
                        {t('sub.change_tariff')}
                    </Menu.Item>
                </>
            )}
            {!inView && (
                <Menu.Item leftSection={<IconPencil size={16} />} onClick={() => openViewAddonModal(addon.id, addon.subscription_id)}>
                    {t('common.edit')}
                </Menu.Item>
            )}
            {addon.rw_user && (
                <Menu.Item
                    leftSection={disabled ? <IconPlayerPlay size={16} /> : <IconPlayerPause size={16} />}
                    onClick={() => toggle('addon', addon.id, disabled)}
                >
                    {disabled ? t('actions.enable') : t('actions.disable')}
                </Menu.Item>
            )}
            <Menu.Divider />
            <Menu.Label>{t('common.danger_zone')}</Menu.Label>
            <Menu.Item color="red" leftSection={<IconLinkOff size={16} />} onClick={() => unlinkAddon(addon, onUnlinked)}>
                {t('actions.unlink')}
            </Menu.Item>
        </>
    )
}

function DotsTarget() {
    const { t } = useTranslation()
    return (
        <Menu.Target>
            <ActionIcon aria-label={t('common.actions')} color="gray" onClick={(e) => e.stopPropagation()} variant="subtle">
                <IconDots size={18} />
            </ActionIcon>
        </Menu.Target>
    )
}

export function SubscriptionActions({ sub }: { sub: Subscription }) {
    return (
        <Menu position="bottom-end" withinPortal>
            <DotsTarget />
            <Menu.Dropdown>
                <SubscriptionMenuItems sub={sub} />
            </Menu.Dropdown>
        </Menu>
    )
}

export function AddonActions({ addon, subTitle }: { addon: AddonItem; subTitle: string }) {
    return (
        <Menu position="bottom-end" withinPortal>
            <DotsTarget />
            <Menu.Dropdown>
                <AddonMenuItems addon={addon} subTitle={subTitle} />
            </Menu.Dropdown>
        </Menu>
    )
}
