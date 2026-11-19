import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Badge, Skeleton, Tabs } from '@mantine/core'
import { IconBinaryTree, IconCoins } from '@tabler/icons-react'
import { useMemo } from 'react'

import { useAccruals, useReferralTree, useSettings } from '@/api/hooks'
import type { Accrual, ReferralNode } from '@/api/types'
import { fmtDate, fmtMoney } from '@/components/format'
import { Money, PageHeader } from '@/components/ui'
import { Page } from '@shared/ui/page'
import { DataTableCard } from '@shared/ui/table'
import { StatStrip } from '@shared/ui/stat-strip'
import { ReferralFlow } from '@/components/referral-flow'
import { useTranslation } from 'react-i18next'

export function ReferralsPage() {
    const { t } = useTranslation()
    const tree = useReferralTree()
    const accruals = useAccruals()
    const settings = useSettings()
    const totals = useMemo(() => (accruals.data ?? []).reduce((s, a) => s + a.amount, 0), [accruals.data])
    const stats = useMemo(() => {
        let referrers = 0
        let referred = 0
        let monthly = 0
        const walk = (n: ReferralNode, root: boolean) => {
            if (n.children.length) referrers++
            if (!root) {
                referred++
                if (!n.archived) monthly += n.monthly
            }
            n.children.forEach((c) => walk(c, false))
        }
        ;(tree.data ?? []).forEach((r) => walk(r, true))
        return { referrers, referred, monthly }
    }, [tree.data])

    const columns = useMemo<MRT_ColumnDef<Accrual>[]>(
        () => [
            { id: 'date', header: t('customer.col_date'), sortingFn: 'datetime', accessorFn: (r) => new Date(r.date), enableColumnFilter: false, Cell: ({ row }) => fmtDate(row.original.date) },
            { accessorKey: 'referrer_name', header: t('referrals.col_to'), filterVariant: 'multi-select' },
            { accessorKey: 'referee_name', header: t('referrals.col_for') },
            { accessorKey: 'percent', header: '%' },
            { accessorKey: 'amount', header: t('customer.col_amount'), Cell: ({ cell }) => <Money value={cell.getValue<number>()} digits={2} /> },
            { accessorKey: 'status', header: t('dashboard.col_status'), Cell: () => <Badge variant="soft" color="gray">{t('referrals.recorded')}</Badge> }
        ],
        [t]
    )

    return (
        <Page title={t('menu.referrals')}>
            <PageHeader
                icon={<IconBinaryTree size={24} />}
                title={t('menu.referrals')}
                description={t('referrals.description', { pct: settings.data?.referral_percent ?? '…', total: fmtMoney(totals, 2) })}
            />
            <StatStrip
                items={[
                    { label: t('referrals.stat_referrers'), value: stats.referrers },
                    { label: t('referrals.stat_referred'), value: stats.referred },
                    { label: t('referrals.stat_referred_monthly'), value: fmtMoney(stats.monthly) },
                    { label: t('referrals.stat_accrued'), value: fmtMoney(totals, 2) }
                ]}
                mb="lg"
            />
            <Tabs defaultValue="tree">
                <Tabs.List mb="md">
                    <Tabs.Tab value="tree">{t('referrals.tree')}</Tabs.Tab>
                    <Tabs.Tab value="accruals">{t('referrals.accruals')}</Tabs.Tab>
                </Tabs.List>
                <Tabs.Panel value="tree">
                    {tree.data ? <ReferralFlow roots={tree.data} /> : <Skeleton h={520} radius="lg" />}
                </Tabs.Panel>
                <Tabs.Panel value="accruals">
                    <DataTableCard
                        storageKey="accruals"
                        icon={<IconCoins size={24} />}
                        title={t('referrals.accruals')}
                        description={t('referrals.accruals_hint')}
                        columns={columns}
                        data={accruals.data ?? []}
                        state={{ isLoading: !accruals.data }}
                        initialState={{ sorting: [{ id: 'date', desc: true }] }}
                    />
                </Tabs.Panel>
            </Tabs>
        </Page>
    )
}
