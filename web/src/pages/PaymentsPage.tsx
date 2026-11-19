import { Button } from '@mantine/core'
import dayjs from 'dayjs'
import { IconCreditCard, IconPlus } from '@tabler/icons-react'
import { useMemo } from 'react'

import { usePayments } from '@/api/hooks'
import { fmtMoney } from '@/components/format'
import { paymentColumns, paymentTableProps } from '@/components/PaymentTable'
import { PageHeader } from '@/components/ui'
import { openPaymentModal } from '@/modals/PaymentModal'
import { StatStrip } from '@shared/ui/stat-strip'
import { Page } from '@shared/ui/page'
import { DataTableCard } from '@shared/ui/table'
import { useTranslation } from 'react-i18next'


export function PaymentsPage() {
    const { t } = useTranslation()
    const { data, isFetching } = usePayments()
    const columns = useMemo(() => paymentColumns(true), [t])
    const stats = useMemo(() => {
        const rows = data ?? []
        const month = dayjs().startOf('month')
        const thisMonth = rows.filter((p) => !dayjs(p.date).isBefore(month))
        const sum = (xs: typeof rows) => xs.reduce((s, p) => s + p.amount, 0)
        return { total: sum(rows), count: rows.length, month: sum(thisMonth), monthCount: thisMonth.length }
    }, [data])

    return (
        <Page title={t('menu.payments')}>
            <PageHeader
                actions={
                    <Button leftSection={<IconPlus size={16} />} onClick={() => openPaymentModal()} variant="filled">
                        {t('customer.record_payment')}
                    </Button>
                }
                description={t('payments.description')}
                icon={<IconCreditCard size={24} />}
                title={t('menu.payments')}
            />
            <StatStrip
                items={[
                    { label: t('payments.total'), value: fmtMoney(stats.total, 2) },
                    { label: t('payments.for_month', { month: dayjs().format('MMMM') }), value: fmtMoney(stats.month, 2), hint: t('payments.count', { count: stats.monthCount }) },
                    { label: t('payments.payments'), value: stats.count }
                ]}
                mb="lg"
            />
            <DataTableCard
                {...paymentTableProps}
                columns={columns}
                data={data ?? []}
                description={t('payments.table_hint')}
                icon={<IconCreditCard size={24} />}
                state={{ showProgressBars: isFetching, isLoading: !data }}
                storageKey="payments"
                title={t('payments.table_title')}
            />
        </Page>
    )
}
