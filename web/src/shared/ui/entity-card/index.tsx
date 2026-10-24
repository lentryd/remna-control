import { ActionIcon, Box, Card, Group, Menu, Text, Tooltip, UnstyledButton } from '@mantine/core'
import { IconDots, IconPlus } from '@tabler/icons-react'
import type { CSSProperties, ReactNode } from 'react'

import { Dot } from '@/components/badges'

import classes from './entity-card.module.css'

// EntityGrid lays out catalog cards (tariffs, tokens, expense items) in
// as many columns as fit.
export function EntityGrid({ min, children }: { min?: number; children: ReactNode }) {
    return (
        <div className={classes.grid} style={min ? ({ '--entity-min': `${min}px` } as CSSProperties) : undefined}>
            {children}
        </div>
    )
}

export interface EntityMenuItem {
    label: ReactNode
    icon?: React.ComponentType<{ size?: number }>
    color?: string
    onClick: () => void
    disabled?: boolean
}

// EntityCard is one catalog entry: a status dot and name with an optional
// ⋯ menu, free body content, dashed-off params and a footer strip.
export function EntityCard({
    title,
    dot,
    badges,
    subtitle,
    menu,
    onClick,
    dimmed,
    children,
    params,
    footer
}: {
    title: ReactNode
    dot?: string
    badges?: ReactNode
    subtitle?: ReactNode
    menu?: EntityMenuItem[]
    onClick?: () => void
    dimmed?: boolean
    children?: ReactNode
    params?: ReactNode
    footer?: ReactNode
}) {
    return (
        <Card className={classes.card} data-clickable={onClick ? true : undefined} data-dimmed={dimmed || undefined} onClick={onClick} padding={0}>
            <div className={classes.body}>
                <Group align="flex-start" justify="space-between" wrap="nowrap">
                    <Box miw={0}>
                        <Group gap={8} wrap="nowrap">
                            {dot && <Dot color={dot} />}
                            <Text c="var(--app-text-strong)" fw={600} truncate="end">
                                {title}
                            </Text>
                            {badges}
                        </Group>
                        {subtitle && (
                            <Text c="dimmed" component="div" lineClamp={2} mt={2} size="xs">
                                {subtitle}
                            </Text>
                        )}
                    </Box>
                    {menu && menu.length > 0 && (
                        <Menu position="bottom-end" withinPortal>
                            <Menu.Target>
                                <ActionIcon mr={-6} mt={-4} onClick={(e) => e.stopPropagation()} size="md">
                                    <IconDots size={18} />
                                </ActionIcon>
                            </Menu.Target>
                            <Menu.Dropdown onClick={(e) => e.stopPropagation()}>
                                {menu.map((m, i) => (
                                    <Menu.Item color={m.color} disabled={m.disabled} key={i} leftSection={m.icon && <m.icon size={15} />} onClick={m.onClick}>
                                        {m.label}
                                    </Menu.Item>
                                ))}
                            </Menu.Dropdown>
                        </Menu>
                    )}
                </Group>
                {children}
                {params && (
                    <div className={classes.paramsWrap}>
                        <div className={classes.params}>{params}</div>
                    </div>
                )}
            </div>
            {footer && <div className={classes.footer}>{footer}</div>}
        </Card>
    )
}

// EntityFigure is the card's headline number with a muted unit after it.
export function EntityFigure({ value, unit, mt = 'md' }: { value: ReactNode; unit?: ReactNode; mt?: string | number }) {
    return (
        <Group align="baseline" gap={6} mt={mt} wrap="nowrap">
            <span className={classes.figure}>{value}</span>
            {unit && (
                <Text c="dimmed" size="sm">
                    {unit}
                </Text>
            )}
        </Group>
    )
}

// EntityParam is one icon + value line in the params block; the label
// shows on hovering the icon, so values may carry their own tooltips.
export function EntityParam({ icon: Icon, label, children }: { icon: React.ComponentType<{ size?: number }>; label: string; children: ReactNode }) {
    return (
        <Group gap={8} maw="100%" wrap="nowrap">
            <Tooltip label={label} position="left">
                <Box c="var(--app-text-faint)" display="flex" style={{ flexShrink: 0 }}>
                    <Icon size={15} />
                </Box>
            </Tooltip>
            <Text component="div" fw={500} miw={0} size="xs">
                {children}
            </Text>
        </Group>
    )
}

// EntityAdd is the dashed "add one more" tile at the end of a grid.
export function EntityAdd({ label, onClick }: { label: ReactNode; onClick: () => void }) {
    return (
        <UnstyledButton className={classes.add} onClick={onClick}>
            <IconPlus size={20} stroke={1.75} />
            {label}
        </UnstyledButton>
    )
}
