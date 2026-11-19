import { Badge, Box, Group, Progress, Stack, Text, Tooltip, type BadgeProps } from '@mantine/core'
import dayjs from 'dayjs'

import type { RwUser } from '@/api/types'
import i18n from '@/app/i18n/i18n'

import { dateLayout, daysLeft, fmtBytes, fmtDate, fmtDateTime, fromNow, isTrafficLow, isUnlimited, strategyLabel, trafficPct } from './format'
import classes from './badges.module.css'

const statusColor: Record<string, string> = {
    ACTIVE: 'teal',
    DISABLED: 'gray',
    LIMITED: 'orange',
    EXPIRED: 'red',
    UNLINKED: 'yellow',
    DELETED: 'red'
}

// statusLabel names a panel user status (and the app's own pseudo ones).
export const statusLabel = (status: string) =>
    ['ACTIVE', 'DISABLED', 'LIMITED', 'EXPIRED', 'UNLINKED', 'DELETED'].includes(status) ? i18n.t(`status.${status as 'ACTIVE'}`) : status

// cssColor turns a theme colour ("teal", "yellow.4") into a CSS value.
export const cssColor = (c: string) =>
    c.startsWith('var(') || c.startsWith('#') ? c : c.includes('.') ? `var(--mantine-color-${c.replace('.', '-')})` : `var(--mantine-color-${c}-5)`

// Dot is the small status light used in pills and rows.
export function Dot({ color, pulse, size = 7 }: { color: string; pulse?: boolean; size?: number }) {
    return <span className={classes.dot} data-pulse={pulse || undefined} style={{ '--dot-color': cssColor(color), '--dot-size': `${size}px` } as React.CSSProperties} />
}

export function StatusBadge({ user, ...props }: { user: RwUser | null } & Omit<BadgeProps, 'children' | 'color'>) {
    if (!user) return <StatusPill status="UNLINKED" {...props} />
    if (user.deleted) return <StatusPill status="DELETED" {...props} />
    return <StatusPill status={user.status} {...props} />
}

// StatusPill is a status as a tinted pill with a leading dot.
export function StatusPill({ status, size = 'md', ...props }: { status: string } & Omit<BadgeProps, 'children' | 'color'>) {
    const color = statusColor[status] ?? 'gray'
    return (
        <Badge color={color} leftSection={<Dot color={color} size={6} />} radius="xl" size={size === 'lg' ? 'md' : size} variant="soft" {...props}>
            {statusLabel(status)}
        </Badge>
    )
}

export function expiryColor(days: number | null) {
    if (days === null) return 'gray'
    if (days < 0) return 'red'
    if (days <= 3) return 'orange'
    if (days <= 7) return 'yellow'
    return 'teal'
}

// expiryTextColor is expiryColor for text: calm (dimmed) while far away.
const expiryTextColor = (days: number | null) => (days === null || days > 7 ? 'dimmed' : expiryColor(days))

export function expirationText(date: string | null | undefined) {
    if (!date) return i18n.t('expiry.unknown')
    const d = dayjs(date)
    if (isUnlimited(date)) return i18n.t('expiry.forever')
    if (d.isBefore(dayjs())) return i18n.t('expiry.expired', { ago: d.fromNow() })
    return i18n.t('expiry.expires_in', { in: d.fromNow(true) })
}

export function ExpireCell({ date, align = 'center' }: { date: string | null | undefined; align?: 'center' | 'flex-start' | 'flex-end' }) {
    const d = daysLeft(date)
    if (!date || d === null) return <Text c="dimmed">—</Text>
    if (isUnlimited(date))
        return (
            <Tooltip label={i18n.t('expiry.forever_hint', { date: dayjs(date).format(dateLayout()) })}>
                <Stack align={align} gap={0}>
                    <Text fw={600} size="md">
                        ∞
                    </Text>
                    <Text c="teal" size="xs">
                        {i18n.t('expiry.forever')}
                    </Text>
                </Stack>
            </Tooltip>
        )
    return (
        <Tooltip label={fmtDateTime(date)}>
            <Stack align={align} gap={1}>
                <Text className="num" fw={500} size="sm">
                    {fmtDate(date)}
                </Text>
                <Text c={expiryTextColor(d)} fw={d <= 7 ? 500 : undefined} size="xs">
                    {expirationText(date)}
                </Text>
            </Stack>
        </Tooltip>
    )
}

function connectionColor(onlineAt: string | null | undefined) {
    if (!onlineAt) return 'var(--app-text-faint)'
    return dayjs().diff(dayjs(onlineAt), 'second') <= 60 ? 'var(--mantine-color-teal-5)' : 'var(--app-border-strong)'
}

// UsernameCell is a panel user: connection light, name and when the user
// was last online.
export function UsernameCell({ user, title, badge }: { user: RwUser | null; title?: string; badge?: React.ReactNode }) {
    const online = !!user?.online_at && dayjs().diff(dayjs(user.online_at), 'second') <= 60
    return (
        <Group align="center" gap="sm" wrap="nowrap">
            <Dot color={user ? connectionColor(user.online_at) : 'var(--app-border-strong)'} pulse={online} size={8} />
            <Box miw={0} w="100%">
                <Group gap={6} wrap="nowrap">
                    {badge}
                    <Text fw={500} size="sm" truncate="end">
                        {title ?? user?.username ?? '—'}
                    </Text>
                </Group>
                <Text c="dimmed" size="xs" truncate="end">
                    {!user ? i18n.t('users.not_in_panel') : user.online_at ? fromNow(user.online_at) : i18n.t('users.never_connected')}
                </Text>
            </Box>
        </Group>
    )
}

// TrafficInfo is what the traffic helpers need of a panel user.
export type TrafficInfo = Pick<RwUser, 'status' | 'used_traffic_bytes' | 'traffic_limit_bytes' | 'next_traffic_reset_at' | 'last_traffic_reset_at'>

// trafficColor is the usage bar color.
export const trafficColor = (pct: number | null) => (pct === null ? 'teal' : pct > 95 ? 'red' : pct > 80 ? 'yellow' : 'brand')

// trafficResetColor highlights the reset of a user running out of traffic:
// yellow while a reset will free it, red if nothing will.
export const trafficResetColor = (user: TrafficInfo) => (!isTrafficLow(user) ? 'dimmed' : user.next_traffic_reset_at ? 'yellow' : 'red')

export function trafficResetText(user: TrafficInfo) {
    if (!user.next_traffic_reset_at) return i18n.t('traffic.no_reset')
    return i18n.t('traffic.resets_in', { in: dayjs(user.next_traffic_reset_at).fromNow(true) })
}

// trafficHint is the tooltip of a limited user's traffic: what's left and
// when it was and will be reset.
export function trafficHint(user: TrafficInfo) {
    const left = Math.max(0, user.traffic_limit_bytes - user.used_traffic_bytes)
    return [
        i18n.t('traffic.left', { left: fmtBytes(left) }),
        user.next_traffic_reset_at ? i18n.t('traffic.next_reset', { date: fmtDateTime(user.next_traffic_reset_at) }) : i18n.t('traffic.no_reset'),
        user.last_traffic_reset_at && i18n.t('traffic.last_reset', { date: fmtDateTime(user.last_traffic_reset_at) })
    ]
        .filter(Boolean)
        .map((line) => <div key={line as string}>{line}</div>)
}

// TrafficCell is the data-usage column: used of limit, a meter and when
// the traffic is reset next — highlighted for users running out of it.
export function TrafficCell({ user, withReset = true }: { user: RwUser | null; withReset?: boolean }) {
    if (!user) return <Text c="dimmed">—</Text>
    const limit = user.traffic_limit_bytes
    const unlimited = !limit
    const pct = trafficPct(user) ?? 0
    const strategy = user.traffic_limit_strategy === 'NO_RESET' ? '' : strategyLabel(user.traffic_limit_strategy)

    const cell = (
        <Box miw={190} w="100%">
            <Group gap={6} justify="space-between" wrap="nowrap">
                <Text className="num" size="xs">
                    <Text component="span" fw={600} inherit>
                        {fmtBytes(user.used_traffic_bytes)}
                    </Text>
                    <Text c="dimmed" component="span" inherit>
                        {' / '}
                        {unlimited ? '∞' : fmtBytes(limit)}
                    </Text>
                </Text>
                {!unlimited && (
                    <Text c={pct > 80 ? trafficColor(pct) : 'dimmed'} className="num" fw={600} size="xs">
                        {Math.round(pct)}%
                    </Text>
                )}
            </Group>
            <Progress color={unlimited ? 'teal' : trafficColor(pct)} mt={5} size={5} value={unlimited ? 100 : Math.min(100, pct)} />
            {withReset && !unlimited && (
                <Text c={trafficResetColor(user)} mt={4} size="xs">
                    {trafficResetText(user)}
                    {strategy && trafficResetColor(user) === 'dimmed' ? ` · ${strategy}` : ''}
                </Text>
            )}
        </Box>
    )
    if (unlimited) return cell
    return <Tooltip label={trafficHint(user)}>{cell}</Tooltip>
}

// TrafficMini is a one-line usage meter for tight rows (add-ons on cards).
export function TrafficMini({ user, w = 150, withReset = true }: { user: TrafficInfo | null; w?: number | string; withReset?: boolean }) {
    if (!user) return null
    const limit = user.traffic_limit_bytes
    const pct = trafficPct(user)
    const body = (
        <Stack gap={4} w={w}>
            <Group gap={4} justify="space-between" wrap="nowrap">
                <Text c="dimmed" className="num" size="xs">
                    {fmtBytes(user.used_traffic_bytes)} / {limit ? fmtBytes(limit) : '∞'}
                </Text>
                {pct !== null && (
                    <Text c={pct > 80 ? trafficColor(pct) : 'dimmed'} className="num" fw={600} size="xs">
                        {Math.round(pct)}%
                    </Text>
                )}
            </Group>
            <Progress color={trafficColor(pct)} size={4} value={pct === null ? 100 : Math.min(100, pct)} />
            {withReset && limit > 0 && (
                <Text c={trafficResetColor(user)} size="xs">
                    {trafficResetText(user)}
                </Text>
            )}
        </Stack>
    )
    return limit ? <Tooltip label={trafficHint(user)}>{body}</Tooltip> : body
}

// lastSeenColor: green within 5 minutes, yellow within an hour, red otherwise.
export function lastSeenColor(onlineAt: string | null | undefined) {
    const minutes = onlineAt ? dayjs().diff(dayjs(onlineAt), 'minute') : null
    if (minutes === null) return 'gray'
    if (minutes <= 5) return 'teal'
    if (minutes <= 60) return 'yellow'
    return 'red'
}

export function OnlineCell({ user }: { user: RwUser | null }) {
    if (!user?.online_at) return <Text c="dimmed" size="sm">{i18n.t('common.never')}</Text>
    return (
        <Tooltip label={fmtDateTime(user.online_at)}>
            <Group gap={8} wrap="nowrap">
                <Dot color={lastSeenColor(user.online_at)} />
                <Text size="sm" style={{ whiteSpace: 'nowrap' }}>
                    {fromNow(user.online_at)}
                </Text>
            </Group>
        </Tooltip>
    )
}
