import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Anchor, Badge, Text } from '@mantine/core'

import type { LedgerEntry } from '@/api/types'
import i18n from '@/app/i18n/i18n'

import { fmtDateTime, fmtMoney } from './format'

const ledgerColor: Record<string, string> = { payment: 'teal', charge: 'orange', adjustment: 'blue', refund: 'grape', company_charge: 'orange' }
const ledgerLabel = (type: string) =>
    ['payment', 'charge', 'adjustment', 'refund', 'referral', 'company_charge'].includes(type) ? i18n.t(`customer.ledger.${type as 'payment'}`) : type

// ledgerColumns are the balance history columns, shared by the customer
// card and the company card; a company_charge row opens its statement.
// Columns are built per render language.
export const ledgerColumns = (onStatement?: (statementId: number) => void): MRT_ColumnDef<LedgerEntry>[] => [
    {
        id: 'date',
        header: i18n.t('customer.col_date'),
        sortingFn: 'datetime',
        accessorFn: (r) => new Date(r.date),
        Cell: ({ row }) => (
            <Text className="num" size="sm">
                {fmtDateTime(row.original.date)}
            </Text>
        )
    },
    {
        accessorKey: 'type',
        header: i18n.t('customer.col_operation'),
        Cell: ({ row }) => (
            <Badge color={ledgerColor[row.original.type]} size="md">
                {ledgerLabel(row.original.type)}
            </Badge>
        )
    },
    {
        accessorKey: 'amount',
        header: i18n.t('customer.col_amount'),
        Cell: ({ cell }) => (
            <Text c={cell.getValue<number>() > 0 ? 'teal' : 'red'} className="num" fw={600} size="sm">
                {cell.getValue<number>() > 0 ? '+' : ''}
                {fmtMoney(cell.getValue<number>(), 2)}
            </Text>
        )
    },
    {
        accessorKey: 'note',
        header: i18n.t('customer.col_note'),
        size: 320,
        Cell: ({ row }) => {
            const sid = row.original.statement_id
            return (
                <Text size="sm">
                    {row.original.note}
                    {sid && onStatement && (
                        <>
                            {' '}
                            <Anchor
                                component="button"
                                onClick={(e) => {
                                    e.stopPropagation()
                                    onStatement(sid)
                                }}
                                size="sm"
                                type="button"
                            >
                                {i18n.t('companies.open_statement')}
                            </Anchor>
                        </>
                    )}
                </Text>
            )
        }
    }
]
