// Adapted from remnawave/frontend (AGPL-3.0): api-tokens-card/modals/scopes.utils
import { createElement, ReactNode } from 'react'
import { IconAffiliate, IconApi, IconChartBar, IconCreditCard, IconDatabase, IconPackage, IconPuzzle, IconReceipt, IconRefresh, IconServer, IconTag, IconUsers } from '@tabler/icons-react'

export interface ScopeEndpoint {
    key: string
    method: string
    path: string
    description: string
    kind: 'read' | 'write'
}

export interface ScopeResource {
    resource: string
    endpoints: ScopeEndpoint[]
}

export type KindState = 'none' | 'off' | 'on' | 'partial'

const RESOURCE_ICONS: Record<string, React.ComponentType<{ size?: number }>> = {
    customers: IconUsers,
    payments: IconCreditCard,
    subscriptions: IconPackage,
    items: IconRefresh,
    tariffs: IconTag,
    addons: IconPuzzle,
    referrals: IconAffiliate,
    expenses: IconReceipt,
    remnawave: IconServer,
    stats: IconChartBar,
    backups: IconDatabase
}

export const renderResourceIcon = (resource: string, size = 18): ReactNode => createElement(RESOURCE_ICONS[resource] ?? IconApi, { size })

export const humanizeResource = (resource: string): string =>
    resource
        .split('-')
        .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
        .join(' ')

export const getMethodColor = (method: string): string => {
    switch (method.toUpperCase()) {
        case 'DELETE':
            return 'red'
        case 'GET':
            return 'blue'
        case 'PATCH':
            return 'yellow'
        case 'POST':
            return 'teal'
        case 'PUT':
            return 'grape'
        default:
            return 'gray'
    }
}

export const getReadKeys = (resource: ScopeResource): string[] => resource.endpoints.filter((e) => e.kind === 'read').map((e) => e.key)

export const getWriteKeys = (resource: ScopeResource): string[] => resource.endpoints.filter((e) => e.kind === 'write').map((e) => e.key)

export const getAllKeys = (resource: ScopeResource): string[] => resource.endpoints.map((e) => e.key)

export const expandScopesToKeys = (resources: ScopeResource[], scopes: string[]): string[] => {
    const byName = new Map(resources.map((r) => [r.resource, r]))
    const keys = new Set<string>()
    for (const scope of scopes) {
        if (scope === '*') {
            resources.forEach((r) => r.endpoints.forEach((e) => keys.add(e.key)))
            continue
        }
        const separator = scope.indexOf(':')
        const resource = byName.get(separator === -1 ? scope : scope.slice(0, separator))
        const rest = separator === -1 ? '' : scope.slice(separator + 1)
        if (!resource) continue
        if (rest === '*') getAllKeys(resource).forEach((k) => keys.add(k))
        else if (rest === 'read') getReadKeys(resource).forEach((k) => keys.add(k))
        else if (rest === 'write') getWriteKeys(resource).forEach((k) => keys.add(k))
        else if (resource.endpoints.some((e) => e.key === scope)) keys.add(scope)
    }
    return Array.from(keys)
}

export const getKindState = (keys: string[], selected: Set<string>): KindState => {
    if (keys.length === 0) return 'none'
    const n = keys.filter((k) => selected.has(k)).length
    if (n === 0) return 'off'
    if (n === keys.length) return 'on'
    return 'partial'
}

export const countSelected = (endpoints: ScopeEndpoint[], selected: Set<string>): number =>
    endpoints.reduce((acc, e) => acc + (selected.has(e.key) ? 1 : 0), 0)

const buildResourceScopes = (resource: ScopeResource, selectedEndpoints: Set<string>): string[] => {
    const selected = resource.endpoints.filter((e) => selectedEndpoints.has(e.key))
    if (selected.length === 0) return []
    if (selected.length === resource.endpoints.length) return [`${resource.resource}:*`]

    const reads = resource.endpoints.filter((e) => e.kind === 'read')
    const writes = resource.endpoints.filter((e) => e.kind === 'write')
    const scopes: string[] = []
    const covered = new Set<string>()
    if (reads.length > 0 && reads.every((e) => selectedEndpoints.has(e.key))) {
        scopes.push(`${resource.resource}:read`)
        reads.forEach((e) => covered.add(e.key))
    }
    if (writes.length > 0 && writes.every((e) => selectedEndpoints.has(e.key))) {
        scopes.push(`${resource.resource}:write`)
        writes.forEach((e) => covered.add(e.key))
    }
    selected.forEach((e) => {
        if (!covered.has(e.key)) scopes.push(e.key)
    })
    return scopes
}

export const buildScopes = (resources: ScopeResource[], selectedEndpoints: Set<string>): string[] => {
    const allSelected = resources.length > 0 && resources.every((r) => r.endpoints.every((e) => selectedEndpoints.has(e.key)))
    if (allSelected) return ['*']
    return resources.flatMap((r) => buildResourceScopes(r, selectedEndpoints))
}
