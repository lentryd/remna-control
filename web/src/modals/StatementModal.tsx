import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Alert, Group, Stack, Text } from '@mantine/core'
import { IconReceipt2 } from '@tabler/icons-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { useCompanyStatement } from '@/api/hooks'
import type { StatementLine } from '@/api/types'
import i18n from '@/app/i18n/i18n'
import { fmtDate, fmtDateTime, fmtMoney, lastDayOf } from '@/components/format'
import { errorText } from '@/components/notify'
import { Money } from '@/components/ui'
import { LoadingScreen } from '@shared/ui/loading-screen'
import { DataTableCard } from '@shared/ui/table'

import { openModal } from './open'

// openStatementModal shows one company statement: a line per employee and
// per tariff segment.
export function openStatementModal(companyId: number, statementId: number) {
    openModal({ icon: IconReceipt2, color: 'orange', title: i18n.t('companies.statement') }, () => <StatementView companyId={companyId} statementId={statementId} />, '1000px')
}

const num = (v: string) => (
    <Text className="num" size="sm">
        {v}
    </Text>
)

export function StatementView({ companyId, statementId }: { companyId: number; statementId: number }) {
    const { t } = useTranslation()
    const { data, isPending, error } = useCompanyStatement(companyId, statementId)
    const columns = useMemo<MRT_ColumnDef<StatementLine>[]>(
        () => [
            { accessorKey: 'username', header: t('companies.username'), Cell: ({ cell }) => <Text fw={500} size="sm">{cell.getValue<string>()}</Text> },
            { accessorKey: 'tariff_name', header: t('companies.tariff') },
            {
                id: 'dates',
                header: t('companies.dates'),
                accessorFn: (r) => r.from,
                Cell: ({ row }) => num(`${fmtDate(row.original.from)} → ${fmtDate(lastDayOf(row.original.to))}`)
            },
            { id: 'days', header: t('companies.days'), accessorFn: (r) => r.days, Cell: ({ row }) => num(`${row.original.days} / ${row.original.days_in_period}`) },
            { accessorKey: 'monthly_price', header: t('companies.price_month'), Cell: ({ cell }) => num(fmtMoney(cell.getValue<number>(), 2)) },
            { accessorKey: 'amount', header: t('customer.col_amount'), Cell: ({ cell }) => <Text className="num" fw={600} size="sm">{fmtMoney(cell.getValue<number>(), 2)}</Text> }
        ],
        [t]
    )
    if (isPending) return <LoadingScreen height="30vh" />
    if (error || !data) return <Alert color="red">{error ? errorText(error) : t('errors.company.not_found')}</Alert>
    return (
        <Stack gap="md">
            <Group gap="xl">
                <div>
                    <Text c="dimmed" size="xs">
                        {t('companies.period')}
                    </Text>
                    <Text className="num" fw={500}>
                        {fmtDate(data.period_from)} → {fmtDate(lastDayOf(data.period_to))}
                    </Text>
                </div>
                <div>
                    <Text c="dimmed" size="xs">
                        {t('companies.total')}
                    </Text>
                    <Text fw={600}>
                        <Money digits={2} value={data.total} />
                    </Text>
                </div>
                <div>
                    <Text c="dimmed" size="xs">
                        {t('companies.generated')}
                    </Text>
                    <Text size="sm">
                        {fmtDateTime(data.created_at)} · {data.generated_by}
                    </Text>
                </div>
            </Group>
            <DataTableCard
                compact
                columns={columns}
                data={data.lines ?? []}
                enablePagination={false}
                initialState={{ pagination: { pageIndex: 0, pageSize: 500 } }}
                storageKey="company-statement-lines"
            />
        </Stack>
    )
}
