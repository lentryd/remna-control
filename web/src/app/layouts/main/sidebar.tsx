import { ActionIcon, Kbd, ScrollArea, Tooltip, UnstyledButton } from '@mantine/core'
import { spotlight } from '@mantine/spotlight'
import { IconLayoutSidebarLeftCollapse, IconLayoutSidebarLeftExpand, IconSearch } from '@tabler/icons-react'
import clsx from 'clsx'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router'

import { Wordmark } from '@shared/ui/logo'

import { isActive, NAV, SETTINGS_ITEM, type NavItem } from './navigation'
import classes from './shell.module.css'
import { SyncStatus } from './sync-status'
import { UserMenu } from './user-menu'

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

function NavLink({ item, collapsed, onNavigate }: { item: NavItem; collapsed: boolean; onNavigate?: () => void }) {
    const { t } = useTranslation()
    const { pathname } = useLocation()
    const label = t(item.label as never) as string
    return (
        <Tooltip disabled={!collapsed} label={label} position="right">
            <Link className={classes.link} data-active={isActive(pathname, item.to) || undefined} onClick={onNavigate} to={item.to}>
                <item.icon className={classes.linkIcon} stroke={1.75} />
                <span className={classes.linkLabel}>{label}</span>
            </Link>
        </Tooltip>
    )
}

interface SidebarProps {
    collapsed: boolean
    onToggleCollapsed?: () => void
    // on phones the sidebar is a drawer that closes on navigation
    onNavigate?: () => void
}

export function Sidebar({ collapsed, onToggleCollapsed, onNavigate }: SidebarProps) {
    const { t } = useTranslation()
    return (
        <div className={clsx({ [classes.collapsed]: collapsed })} style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
            <div className={classes.brand} data-mobile={!onToggleCollapsed || undefined}>
                <Link aria-label="Remna Control" className={classes.brandLink} onClick={onNavigate} to="/">
                    <Wordmark compact={collapsed} />
                </Link>
                {onToggleCollapsed && !collapsed && (
                    <Tooltip label={t('shell.collapse')}>
                        <ActionIcon aria-label={t('shell.collapse')} onClick={onToggleCollapsed} size="md">
                            <IconLayoutSidebarLeftCollapse size={18} stroke={1.75} />
                        </ActionIcon>
                    </Tooltip>
                )}
            </div>

            <Tooltip disabled={!collapsed} label={t('shell.search')} position="right">
                <UnstyledButton className={classes.search} onClick={() => spotlight.open()}>
                    <IconSearch size={15} stroke={1.75} />
                    {!collapsed && (
                        <>
                            <span className={classes.searchText}>{t('shell.search')}</span>
                            <Kbd size="xs">{isMac ? '⌘K' : 'Ctrl K'}</Kbd>
                        </>
                    )}
                </UnstyledButton>
            </Tooltip>

            <ScrollArea className={classes.scroll} scrollbarSize={4} type="hover">
                <nav className={classes.groups}>
                    {NAV.map((group) => (
                        <div className={classes.group} key={group.id}>
                            {group.label && <div className={classes.groupLabel}>{t(group.label as never)}</div>}
                            <div className={classes.items}>
                                {group.items.map((item) => (
                                    <NavLink collapsed={collapsed} item={item} key={item.to} onNavigate={onNavigate} />
                                ))}
                            </div>
                        </div>
                    ))}
                </nav>
            </ScrollArea>

            <div className={classes.footer}>
                <NavLink collapsed={collapsed} item={SETTINGS_ITEM} onNavigate={onNavigate} />
                {onToggleCollapsed && collapsed && (
                    <Tooltip label={t('shell.expand')} position="right">
                        <UnstyledButton aria-label={t('shell.expand')} className={classes.link} onClick={onToggleCollapsed}>
                            <IconLayoutSidebarLeftExpand className={classes.linkIcon} stroke={1.75} />
                        </UnstyledButton>
                    </Tooltip>
                )}
                <SyncStatus collapsed={collapsed} />
                <UserMenu collapsed={collapsed} />
            </div>
        </div>
    )
}
