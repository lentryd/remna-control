import { ActionIcon, Badge, Box, Button, Card, Group, Text, ThemeIcon, Tooltip, UnstyledButton } from '@mantine/core'
import { IconCalendarPlus, IconPencil, IconPuzzle, IconRepeat, IconStack2 } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import type { RwUser, Subscription } from '@/api/types'
import { openExtendModal } from '@/modals/ExtendModal'
import { openTrafficAnchorModal } from '@/modals/SubscriptionModals'
import { openViewAddonModal, openViewSubscriptionModal } from '@/modals/ViewItemModal'

import { ExpireCell, OnlineCell, StatusBadge, TrafficCell, TrafficMini, trafficHint, trafficResetColor } from './badges'
import { fmtDate, fmtDateTime } from './format'
import { AddonActions, SubscriptionActions } from './ItemActions'
import classes from './subscription-card.module.css'
import { Money } from './ui'

function AutoIcon({ on }: { on: boolean }) {
    const { t } = useTranslation()
    return (
        <Tooltip label={on ? t('card.auto_on') : t('card.auto_off')}>
            <IconRepeat color={on ? 'var(--mantine-color-teal-5)' : 'var(--app-text-faint)'} size={15} stroke={1.75} />
        </Tooltip>
    )
}

function Fact({ label, wide, children }: { label: React.ReactNode; wide?: boolean; children: React.ReactNode }) {
    return (
        <div className={`${classes.fact} ${wide ? classes.wide : ''}`}>
            <div className={classes.factLabel}>{label}</div>
            {children}
        </div>
    )
}

// TrafficResetLabel is the reset date shown next to the traffic caption.
function TrafficResetLabel({ user }: { user: RwUser }) {
    const { t } = useTranslation()
    return (
        <Tooltip label={trafficHint(user)}>
            <Text c={trafficResetColor(user)} component="span" inherit>
                {user.next_traffic_reset_at ? t('traffic.reset_on', { date: fmtDate(user.next_traffic_reset_at) }) : t('traffic.no_reset')}
            </Text>
        </Tooltip>
    )
}

// SubscriptionCard is a subscription on the customer page: header with
// status and actions, a row of facts, and its add-ons underneath. Clicking
// the title opens the subscription.
export function SubscriptionCard({ sub }: { sub: Subscription }) {
    const { t } = useTranslation()
    return (
        <Card className={classes.card} padding={0}>
            <div className={classes.header}>
                <UnstyledButton className={classes.titleButton} onClick={() => openViewSubscriptionModal(sub)}>
                    <Group gap={12} wrap="nowrap">
                        <ThemeIcon radius="md" size={36}>
                            <IconStack2 size={18} stroke={1.75} />
                        </ThemeIcon>
                        <Box miw={0}>
                            <Text c="var(--app-text-strong)" fw={600} size="md" truncate="end">
                                {sub.title}
                            </Text>
                            <Text c="dimmed" size="xs" truncate="end">
                                {sub.rw_user?.username ?? t('view.not_linked')}
                            </Text>
                        </Box>
                    </Group>
                </UnstyledButton>
                <Group gap="sm" wrap="nowrap">
                    <AutoIcon on={sub.auto_extend} />
                    <StatusBadge user={sub.rw_user} />
                    {sub.rw_user && !sub.rw_user.unlimited && (
                        <Button
                            leftSection={<IconCalendarPlus size={14} />}
                            onClick={() => openExtendModal({ kind: 'subscription', id: sub.id, title: sub.title })}
                            size="compact-sm"
                            variant="default"
                        >
                            {t('dashboard.extend')}
                        </Button>
                    )}
                    <SubscriptionActions sub={sub} />
                </Group>
            </div>

            <div className={classes.facts}>
                <Fact label={t('tariffs.tariff')}>
                    <Group gap={6} wrap="nowrap">
                        <Text fw={600} size="sm" truncate="end">
                            {sub.tariff_name || '—'}
                        </Text>
                        {sub.price_override !== null && (
                            <Badge color="yellow" size="xs">
                                {t('card.custom')}
                            </Badge>
                        )}
                    </Group>
                    <Text c="dimmed" size="xs">
                        <Money value={sub.price} /> {t('card.per_month')}
                    </Text>
                </Fact>
                <Fact label={t('sub.paid_until')}>
                    <ExpireCell align="flex-start" date={sub.rw_user?.expire_at} />
                </Fact>
                <Fact
                    label={
                        <>
                            {t('sub.traffic')}
                            {sub.rw_user?.traffic_limit_bytes ? (
                                <>
                                    {' · '}
                                    <TrafficResetLabel user={sub.rw_user} />
                                </>
                            ) : null}
                        </>
                    }
                    wide
                >
                    <TrafficCell user={sub.rw_user} withReset={false} />
                </Fact>
                {sub.traffic_anchor_at && (
                    <Fact label={t('sub.traffic_anchor')}>
                        <Group gap={4} wrap="nowrap">
                            <Text size="sm">{fmtDateTime(sub.traffic_anchor_at)}</Text>
                            <Tooltip label={t('sub.traffic_anchor_edit')}>
                                <ActionIcon
                                    aria-label={t('sub.traffic_anchor_edit')}
                                    onClick={() => openTrafficAnchorModal({ kind: 'subscription', id: sub.id, title: sub.title, anchor: sub.traffic_anchor_at! })}
                                    size="sm"
                                    variant="subtle"
                                >
                                    <IconPencil size={14} />
                                </ActionIcon>
                            </Tooltip>
                        </Group>
                    </Fact>
                )}
                <Fact label={t('card.last_online')}>
                    <OnlineCell user={sub.rw_user} />
                </Fact>
            </div>

            {sub.addons.length > 0 && (
                <>
                    <div className={classes.addonsLabel}>{t('view.addons')}</div>
                    {sub.addons.map((a) => (
                        <div className={classes.addon} key={a.id}>
                            <UnstyledButton className={classes.titleButton} maw="100%" onClick={() => openViewAddonModal(a.id, sub.id)}>
                                <Group gap={10} wrap="nowrap">
                                    <ThemeIcon color="grape" radius="md" size={30}>
                                        <IconPuzzle size={16} stroke={1.75} />
                                    </ThemeIcon>
                                    <Box miw={0}>
                                        <Group gap={6} wrap="nowrap">
                                            <Text fw={600} size="sm" truncate="end">
                                                {a.addon_name}
                                            </Text>
                                            <AutoIcon on={a.auto_extend} />
                                        </Group>
                                        <Text c="dimmed" size="xs" truncate="end">
                                            {a.tariff_name || t('view.no_tariff')}
                                            {' · '}
                                            {a.included ? t('card.in_tariff') : <Money value={a.price} />}
                                            {a.traffic_anchor_at && !a.included && (
                                                <>
                                                    {' · '}
                                                    {t('sub.traffic_anchor')}: {fmtDateTime(a.traffic_anchor_at)}
                                                </>
                                            )}
                                        </Text>
                                    </Box>
                                </Group>
                            </UnstyledButton>
                            <ExpireCell align="flex-start" date={a.rw_user?.expire_at} />
                            <TrafficMini user={a.rw_user} w="100%" />
                            <Group gap="xs" justify="flex-end" wrap="nowrap">
                                <StatusBadge user={a.rw_user} />
                                {a.traffic_anchor_at && !a.included && (
                                    <Tooltip label={t('sub.traffic_anchor_edit')}>
                                        <ActionIcon
                                            aria-label={t('sub.traffic_anchor_edit')}
                                            onClick={() => openTrafficAnchorModal({ kind: 'addon', id: a.id, title: a.addon_name, anchor: a.traffic_anchor_at! })}
                                            size="sm"
                                            variant="subtle"
                                        >
                                            <IconPencil size={14} />
                                        </ActionIcon>
                                    </Tooltip>
                                )}
                                <AddonActions addon={a} subTitle={sub.title} />
                            </Group>
                        </div>
                    ))}
                </>
            )}
        </Card>
    )
}
