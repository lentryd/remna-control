import { Anchor, Group, Text } from '@mantine/core'
import { IconChevronLeft } from '@tabler/icons-react'
import { Link } from 'react-router'
import type { ReactNode } from 'react'

import { MetricCardShared } from '@shared/ui/metrics/metric-card'
import { PageHeaderShared } from '@shared/ui/page-header'

import { fmtMoney } from './format'

// PageHeader is the top of a page: title, description and actions.
export function PageHeader({
    title,
    description,
    actions,
    icon,
    eyebrow,
    badges
}: {
    title: ReactNode
    description?: ReactNode
    actions?: ReactNode
    icon?: ReactNode
    eyebrow?: ReactNode
    badges?: ReactNode
}) {
    return <PageHeaderShared actions={actions} badges={badges} description={description} eyebrow={eyebrow} icon={icon} title={title} wrapActions />
}

// BackLink is a page header's way back to the list it came from.
export function BackLink({ to, children }: { to: string; children: ReactNode }) {
    return (
        <Anchor c="dimmed" component={Link} size="sm" to={to} underline="never" style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
            <IconChevronLeft size={14} stroke={2} />
            {children}
        </Anchor>
    )
}

// StatCard is a KPI tile.
export function StatCard({
    title,
    value,
    hint,
    tone
}: {
    title: string
    value: ReactNode
    hint?: ReactNode
    tone?: string
}) {
    return <MetricCardShared subtitle={hint} title={title} tone={tone} value={value} />
}

export function Money({ value, signed, digits = 0 }: { value: number | null | undefined; signed?: boolean; digits?: 0 | 2 }) {
    if (value === null || value === undefined) return <Text c="dimmed">—</Text>
    const color = signed ? (value > 0 ? 'teal' : value < 0 ? 'red' : undefined) : undefined
    return (
        <Text span c={color} className="num" fw={500} inherit style={{ whiteSpace: 'nowrap' }}>
            {signed && value > 0 ? '+' : ''}
            {fmtMoney(value, digits)}
        </Text>
    )
}

export function SectionTitle({ children, right }: { children: ReactNode; right?: ReactNode }) {
    return (
        <Group justify="space-between" mb="sm">
            <Text fw={600}>{children}</Text>
            {right}
        </Group>
    )
}
