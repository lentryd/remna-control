import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { ActionIcon, Anchor, Badge, Group, Text } from '@mantine/core'
import { IconBolt, IconBuildingBank, IconCash, IconCurrencyBitcoin, IconPencil, IconQuestionMark, IconTrash, IconUser } from '@tabler/icons-react'
import { Link } from 'react-router'

import { api } from '@/api/client'
import { useInvalidateAll } from '@/api/hooks'
import type { Payment } from '@/api/types'
import { openPaymentEdit } from '@/modals/PaymentEditModal'
import { confirmDanger } from '@/modals/open'

import { fmtDate, fmtMoney } from './format'
import { notifyError } from './notify'
import i18n from '@/app/i18n/i18n'

// methodMeta styles the suggested methods of both languages; others are gray.
const bank = { color: 'brand', icon: <IconBuildingBank size={14} /> }
const instant = { color: 'violet', icon: <IconBolt size={14} /> }
const cash = { color: 'teal', icon: <IconCash size={14} /> }
const crypto = { color: 'orange', icon: <IconCurrencyBitcoin size={14} /> }
const methodMeta: Record<string, { color: string; icon: React.ReactNode }> = {
    Перевод: bank,
    'Bank transfer': bank,
    СБП: instant,
    Card: instant,
    Наличные: cash,
    Cash: cash,
    Крипта: crypto,
    Crypto: crypto
}

export function MethodBadge({ method }: { method: string }) {
    if (!method) return <Text c="dimmed">—</Text>
    const m = methodMeta[method] ?? { color: 'gray', icon: <IconQuestionMark size={14} /> }
    return (
        <Badge color={m.color} leftSection={m.icon} size="md">
            {method}
        </Badge>
    )
}

// paymentColumns are shared by the payments page and the customer card.
export function paymentColumns(withCustomer: boolean): MRT_ColumnDef<Payment>[] {
    const cols: MRT_ColumnDef<Payment>[] = [
        {
            id: 'date',
            header: i18n.t('customer.col_date'),
            size: 130,
            sortingFn: 'datetime',
            accessorFn: (r) => new Date(r.date),
            enableColumnFilter: false,
            Cell: ({ row }) => (
                <Text className="num" fw={500} size="sm">
                    {fmtDate(row.original.date)}
                </Text>
            )
        }
    ]
    if (withCustomer) {
        cols.push({
            accessorKey: 'customer_name',
            header: i18n.t('sub.customer'),
            Cell: ({ row }) => (
                <Anchor component={Link} onClick={(e) => e.stopPropagation()} size="sm" to={`/customers/${row.original.customer_id}`}>
                    <Group gap={6} wrap="nowrap">
                        <IconUser size={14} />
                        {row.original.customer_name}
                    </Group>
                </Anchor>
            )
        })
    }
    cols.push(
        {
            accessorKey: 'amount',
            header: i18n.t('customer.col_amount'),
            size: 140,
            enableColumnFilter: false,
            Cell: ({ cell }) => (
                <Text c="teal.4" className="num" fw={600} size="sm" style={{ whiteSpace: 'nowrap' }}>
                    +{fmtMoney(cell.getValue<number>(), 2)}
                </Text>
            )
        },
        {
            accessorKey: 'method',
            header: i18n.t('payment.method'),
            size: 160,
            filterVariant: 'multi-select',
            Cell: ({ cell }) => <MethodBadge method={cell.getValue<string>()} />
        },
        {
            accessorKey: 'note',
            header: i18n.t('payment.comment'),
            size: 300,
            Cell: ({ row }) => (
                <Group gap="xs" wrap="nowrap">
                    <Text c={row.original.note ? undefined : 'dimmed'} size="sm" truncate="end">
                        {row.original.note || '—'}
                    </Text>
                </Group>
            )
        }
    )
    return cols
}

export function PaymentRowActions({ payment }: { payment: Payment }) {
    const invalidate = useInvalidateAll()
    const remove = () =>
        confirmDanger(
            i18n.t('payment.delete'),
            i18n.t('payment.delete_hint'),
            async () => {
                try {
                    await api.del(`payments/${payment.id}`)
                    await invalidate()
                } catch (e) {
                    notifyError(e)
                }
            }
        )
    return (
        <Group gap={2} wrap="nowrap">
            <ActionIcon onClick={() => openPaymentEdit(payment)} size="lg">
                <IconPencil size={18} />
            </ActionIcon>
            <ActionIcon color="red" onClick={remove} size="lg">
                <IconTrash size={18} />
            </ActionIcon>
        </Group>
    )
}

export const paymentTableProps = {
    enableRowActions: true,
    positionActionsColumn: 'last' as const,
    onRowClick: openPaymentEdit,
    renderRowActions: ({ row }: { row: { original: Payment } }) => <PaymentRowActions payment={row.original} />,
    displayColumnDefOptions: { 'mrt-row-actions': { header: '', size: 110 } },
    initialState: { sorting: [{ id: 'date', desc: true }] }
}
