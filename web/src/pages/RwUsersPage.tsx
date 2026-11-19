import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Anchor, Badge, Button, Group, Menu, SegmentedControl } from '@mantine/core'
import { IconChevronDown, IconLink, IconUserPlus, IconUsersGroup } from '@tabler/icons-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router'

import { useRwUsers } from '@/api/hooks'
import type { RwUserRow } from '@/api/types'
import { ExpireCell, StatusBadge, TrafficCell, UsernameCell } from '@/components/badges'
import { trafficPct } from '@/components/format'
import { PageHeader } from '@/components/ui'
import { openCustomerForm } from '@/modals/CustomerModals'
import { openSubscriptionForm } from '@/modals/SubscriptionModals'
import { openViewAddonModal, openViewSubscriptionModal } from '@/modals/ViewItemModal'
import { Page } from '@shared/ui/page'
import { DataTableCard } from '@shared/ui/table'
import { useTranslation } from 'react-i18next'

export function RwUsersPage() {
    const { t } = useTranslation()
    const { data, isFetching } = useRwUsers()
    const [scope, setScope] = useState('unlinked')
    const rows = useMemo(() => (data ?? []).filter((u) => (scope === 'all' ? true : !u.linked)), [data, scope])

    const columns = useMemo<MRT_ColumnDef<RwUserRow>[]>(
        () => [
            { accessorKey: 'username', header: t('expense_items.col_user'), size: 240, Cell: ({ row }) => <UsernameCell user={row.original} /> },
            { accessorKey: 'description', header: t('tariffs.description_label'), size: 160 },
            { id: 'status', header: t('dashboard.col_status'), accessorFn: (r) => r.status, filterVariant: 'multi-select', size: 140, mantineTableBodyCellProps: { align: 'center' }, Cell: ({ row }) => <StatusBadge user={row.original} /> },
            {
                id: 'expire',
                header: t('sub.expires'),
                sortingFn: 'datetime',
                accessorFn: (r) => (r.expire_at ? new Date(r.expire_at) : undefined),
                sortDescFirst: false,
                sortUndefined: 'last',
                enableColumnFilter: false,
                mantineTableBodyCellProps: { align: 'center' },
                Cell: ({ row }) => <ExpireCell date={row.original.expire_at} />
            },
            { id: 'traffic', header: t('sub.traffic'), accessorFn: (r) => trafficPct(r) ?? -1, sortDescFirst: true, enableColumnFilter: false, size: 260, Cell: ({ row }) => <TrafficCell user={row.original} /> },
            {
                id: 'linked',
                header: t('sub.customer'),
                accessorFn: (r) => r.customer_name,
                size: 200,
                Cell: ({ row }) =>
                    row.original.linked ? (
                        <Group gap={6}>
                            <Anchor component={Link} to={`/customers/${row.original.customer_id}`} size="sm">
                                {row.original.customer_name}
                            </Anchor>
                            {row.original.subscription_addon_id && (
                                <Badge size="xs" color="grape" variant="soft">
                                    {t('dashboard.addon_badge')}
                                </Badge>
                            )}
                        </Group>
                    ) : (
                        <LinkMenu user={row.original} />
                    )
            }
        ],
        [t]
    )

    return (
        <Page title={t('menu.panel_users')}>
            <PageHeader
                icon={<IconUsersGroup size={24} />}
                title={t('menu.panel_users')}
                description={t('panel_users.description')}
            />
            <DataTableCard
                storageKey="panel-users"
                icon={<IconUsersGroup size={24} />}
                title={t('panel_users.table_title')}
                actions={
                    <SegmentedControl
                        size="xs"
                        value={scope}
                        onChange={setScope}
                        data={[
                            { value: 'unlinked', label: t('panel_users.unlinked') },
                            { value: 'all', label: t('common.all') }
                        ]}
                    />
                }
                columns={columns}
                data={rows}
                state={{ showProgressBars: isFetching, isLoading: !data }}
                onRowClick={(r) => {
                    if (r.subscription_addon_id && r.parent_subscription_id) openViewAddonModal(r.subscription_addon_id, r.parent_subscription_id)
                    else if (r.subscription_id) openViewSubscriptionModal({ id: r.subscription_id, title: r.username, customer_name: r.customer_name })
                }}
                initialState={{ sorting: [{ id: 'expire', desc: false }] }}
            />
        </Page>
    )
}

// LinkMenu is the customer cell of an unlinked panel user: one button that
// attaches it to an existing customer or to a new one.
function LinkMenu({ user }: { user: RwUserRow }) {
    const { t } = useTranslation()
    return (
        <Menu position="bottom-start" withinPortal>
            <Menu.Target>
                <Button
                    color="yellow"
                    leftSection={<IconLink size={14} />}
                    onClick={(e) => e.stopPropagation()}
                    rightSection={<IconChevronDown size={12} />}
                    size="compact-sm"
                    variant="soft"
                >
                    {t('panel_users.link')}
                </Button>
            </Menu.Target>
            <Menu.Dropdown onClick={(e) => e.stopPropagation()}>
                <Menu.Item leftSection={<IconLink size={15} />} onClick={() => openSubscriptionForm({ rwUserId: user.id })}>
                    {t('panel_users.to_customer')}
                </Menu.Item>
                <Menu.Item
                    leftSection={<IconUserPlus size={15} />}
                    onClick={() => openCustomerForm(undefined, (id) => openSubscriptionForm({ customerId: id, rwUserId: user.id }), { provision: false })}
                >
                    {t('customers.new')}
                </Menu.Item>
            </Menu.Dropdown>
        </Menu>
    )
}
