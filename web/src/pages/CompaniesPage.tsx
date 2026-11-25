import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Badge, Button, Group, SegmentedControl, Text } from '@mantine/core'
import { IconBuilding, IconPlus } from '@tabler/icons-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { useCompanies } from '@/api/hooks'
import type { Company } from '@/api/types'
import { fmtDate, fmtMoney } from '@/components/format'
import { Money, PageHeader } from '@/components/ui'
import { openCompanyForm } from '@/modals/CompanyModals'
import { Page } from '@shared/ui/page'
import { StatStrip } from '@shared/ui/stat-strip'
import { DataTableCard } from '@shared/ui/table'

export function CompaniesPage() {
    const { t } = useTranslation()
    const { data, isFetching } = useCompanies()
    const navigate = useNavigate()
    const [scope, setScope] = useState('active')
    const active = useMemo(() => (data ?? []).filter((c) => !c.archived), [data])
    const rows = useMemo(() => (data ?? []).filter((c) => (scope === 'all' ? true : scope === 'archived' ? c.archived : !c.archived)), [data, scope])

    const columns = useMemo<MRT_ColumnDef<Company>[]>(
        () => [
            {
                accessorKey: 'name',
                header: t('companies.name'),
                Cell: ({ row }) => (
                    <Group gap={6} wrap="nowrap" pl={10}>
                        <Text fw={500} size="sm">
                            {row.original.name}
                        </Text>
                        {row.original.archived && (
                            <Badge color="gray" size="xs" variant="soft">
                                {t('customer.archived')}
                            </Badge>
                        )}
                    </Group>
                )
            },
            { accessorKey: 'tariff_name', header: t('companies.tariff') },
            { accessorKey: 'balance', header: t('dashboard.col_balance'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} signed digits={2} /> },
            { accessorKey: 'members_count', header: t('companies.members'), enableColumnFilter: false },
            {
                accessorKey: 'period_end',
                header: t('companies.next_report'),
                sortingFn: 'datetime',
                accessorFn: (r) => new Date(r.period_end),
                enableColumnFilter: false,
                Cell: ({ row }) => <Text size="sm">{fmtDate(row.original.period_end)}</Text>
            },
            { accessorKey: 'forecast', header: t('companies.forecast'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} digits={2} /> }
        ],
        [t]
    )

    return (
        <Page title={t('menu.companies')}>
            <PageHeader
                icon={<IconBuilding size={24} />}
                title={t('menu.companies')}
                description={t('companies.description')}
                actions={
                    <Button leftSection={<IconPlus size={16} />} onClick={openCompanyForm} variant="filled">
                        {t('companies.new')}
                    </Button>
                }
            />
            <StatStrip
                items={[
                    { label: t('companies.active'), value: data ? active.length : '—', hint: t('customers.stat_total', { count: data?.length ?? 0 }) },
                    { label: t('companies.members'), value: data ? active.reduce((n, c) => n + c.members_count, 0) : '—' },
                    { label: t('companies.forecast'), value: data ? fmtMoney(active.reduce((n, c) => n + c.forecast, 0)) : '—' },
                    {
                        label: t('customers.stat_debtors'),
                        value: data ? active.filter((c) => c.balance < 0).length : '—',
                        color: 'red',
                        hint: data ? fmtMoney(active.filter((c) => c.balance < 0).reduce((n, c) => n + c.balance, 0), 2) : undefined
                    }
                ]}
                mb="lg"
            />
            <DataTableCard
                storageKey="companies"
                icon={<IconBuilding size={24} />}
                title={t('companies.list')}
                actions={
                    <SegmentedControl
                        size="xs"
                        value={scope}
                        onChange={setScope}
                        data={[
                            { value: 'active', label: t('customers.scope_active') },
                            { value: 'archived', label: t('customers.scope_archived') },
                            { value: 'all', label: t('common.all') }
                        ]}
                    />
                }
                columns={columns}
                data={rows}
                state={{ showProgressBars: isFetching, isLoading: !data }}
                initialState={{ sorting: [{ id: 'name', desc: false }] }}
                onRowClick={(r) => navigate(`/companies/${r.id}`)}
            />
        </Page>
    )
}
