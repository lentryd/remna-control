import { ActionIcon, AppShell, Burger } from '@mantine/core'
import { useDisclosure, useLocalStorage, useMediaQuery } from '@mantine/hooks'
import { spotlight } from '@mantine/spotlight'
import { IconSearch } from '@tabler/icons-react'
import { Suspense } from 'react'
import { useTranslation } from 'react-i18next'
import { Outlet, useLocation } from 'react-router'

import { LoadingProgress } from '@shared/ui/loading-screen'
import { Logo } from '@shared/ui/logo'

import { CommandPalette } from './command-palette'
import { pageTitle } from './navigation'
import { OfflineBanner } from './offline-banner'
import classes from './shell.module.css'
import { Sidebar } from './sidebar'

// MainLayout is the signed-in shell: a collapsible sidebar on desktop, a
// slim header with a drawer on phones, and the page in a centred column.
export function MainLayout() {
    const { t } = useTranslation()
    const { pathname } = useLocation()
    const desktop = useMediaQuery('(min-width: 64em)', true, { getInitialValueInEffect: false })
    const [mobileOpened, mobile] = useDisclosure(false)
    const [collapsed, setCollapsed] = useLocalStorage({ key: 'remnac:sidebar-collapsed', defaultValue: false })
    const narrow = desktop && collapsed

    return (
        <AppShell
            header={{ height: 56, collapsed: desktop }}
            navbar={{ width: narrow ? 68 : 248, breakpoint: 'lg', collapsed: { mobile: !mobileOpened } }}
            padding={0}
            transitionDuration={200}
        >
            <AppShell.Header className={classes.header} withBorder={false}>
                <Burger aria-label={t('shell.open_menu')} onClick={mobile.toggle} opened={mobileOpened} size="sm" />
                <Logo size={24} />
                <div className={classes.headerTitle}>{pageTitle(pathname, t)}</div>
                <ActionIcon aria-label={t('shell.search')} onClick={() => spotlight.open()} size="lg">
                    <IconSearch size={18} stroke={1.75} />
                </ActionIcon>
            </AppShell.Header>

            <AppShell.Navbar className={classes.navbar} withBorder={false}>
                <Sidebar
                    collapsed={narrow}
                    onNavigate={desktop ? undefined : mobile.close}
                    onToggleCollapsed={desktop ? () => setCollapsed((c) => !c) : undefined}
                />
            </AppShell.Navbar>

            <AppShell.Main className={classes.main}>
                <div className={classes.content}>
                    <OfflineBanner />
                    <Suspense fallback={<LoadingProgress />}>
                        <Outlet />
                    </Suspense>
                </div>
            </AppShell.Main>

            <CommandPalette />
        </AppShell>
    )
}
