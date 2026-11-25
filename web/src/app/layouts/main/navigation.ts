import {
    IconAffiliate,
    IconBuilding,
    IconBuildingBank,
    IconCreditCard,
    IconLayoutDashboard,
    IconReceipt2,
    IconServer2,
    IconSettings,
    IconStack2,
    IconTags,
    IconUsers,
    type Icon
} from '@tabler/icons-react'
import { matchPath } from 'react-router'

import type { TFunction } from 'i18next'

export interface NavItem {
    to: string
    // locale key of the label
    label: string
    icon: Icon
}

export interface NavGroup {
    id: string
    // locale key of the group caption; none for the top group
    label?: string
    items: NavItem[]
}

// NAV is the sidebar: pages grouped by what they're about.
export const NAV: NavGroup[] = [
    { id: 'overview', items: [{ to: '/', label: 'menu.home', icon: IconLayoutDashboard }] },
    {
        id: 'customers',
        label: 'menu.customers',
        items: [
            { to: '/customers', label: 'menu.customers', icon: IconUsers },
            { to: '/companies', label: 'menu.companies', icon: IconBuilding },
            { to: '/subscriptions', label: 'menu.subscriptions', icon: IconStack2 },
            { to: '/payments', label: 'menu.payments', icon: IconCreditCard },
            { to: '/referrals', label: 'menu.referrals', icon: IconAffiliate }
        ]
    },
    { id: 'catalog', label: 'menu.catalog', items: [{ to: '/tariffs', label: 'menu.tariffs_addons', icon: IconTags }] },
    {
        id: 'finance',
        label: 'menu.finance',
        items: [
            { to: '/expenses', label: 'menu.expenses', icon: IconReceipt2 },
            { to: '/expense-items', label: 'menu.expense_items', icon: IconBuildingBank }
        ]
    },
    { id: 'panel', label: 'menu.panel', items: [{ to: '/panel-users', label: 'menu.panel_users', icon: IconServer2 }] }
]

export const SETTINGS_ITEM: NavItem = { to: '/settings', label: 'menu.settings', icon: IconSettings }

export const ALL_NAV_ITEMS = [...NAV.flatMap((g) => g.items), SETTINGS_ITEM]

export const isActive = (pathname: string, to: string) => matchPath({ path: to, end: to === '/' }, pathname) !== null

// pageTitle names the page at a path, for the mobile header.
export function pageTitle(pathname: string, t: TFunction) {
    const item = ALL_NAV_ITEMS.find((i) => i.to !== '/' && isActive(pathname, i.to)) ?? (pathname === '/' ? ALL_NAV_ITEMS[0] : undefined)
    return item ? (t(item.label as never) as string) : ''
}
