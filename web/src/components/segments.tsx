import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Badge, Group, Text } from '@mantine/core'
import { IconHistory } from '@tabler/icons-react'
import dayjs from 'dayjs'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type { MemberOverrideField, MemberSegment } from '@/api/types'
import { DataTableCard } from '@shared/ui/table'

import { daysBetween, fmtBytes, fmtDate, lastDayOf, strategyLabel } from './format'
import { Money } from './ui'

// Names of the fields a member can override (the keys of the segment).
export const FIELD_LABEL = {
    price: 'companies.price_month',
    traffic_limit: 'tariffs.traffic_limit',
    traffic_strategy: 'tariffs.traffic_reset',
    hwid_limit: 'tariffs.hwid',
    squads: 'tariffs.squads_label',
    external_squad: 'companies.external_squad_label'
} as const

// OverrideBadges lists which fields are set for this member alone.
export function OverrideBadges({ fields }: { fields: MemberOverrideField[] }) {
    const { t } = useTranslation()
    return (
        <>
            {fields.map((f) => (
                <Badge color="grape" key={f} size="xs" variant="soft">
                    {t(FIELD_LABEL[f])}
                </Badge>
            ))}
        </>
    )
}

const num = (v: string) => (
    <Text className="num" size="sm">
        {v}
    </Text>
)

// segmentColumns are the columns of a billing history: a stretch of days and
// what it was billed on. Built per render language.
export function segmentColumns(t: (k: string) => string): MRT_ColumnDef<MemberSegment>[] {
    return [
        {
            id: 'period',
            header: t('companies.period'),
            size: 230,
            sortingFn: 'datetime',
            accessorFn: (r) => new Date(r.from),
            Cell: ({ row }) => {
                const s = row.original
                return num(`${fmtDate(s.from)} → ${s.to ? fmtDate(lastDayOf(s.to)) : t('companies.until_now')}`)
            }
        },
        {
            id: 'days',
            header: t('companies.days'),
            size: 90,
            enableColumnFilter: false,
            accessorFn: (r) => daysBetween(r.from, r.to ?? dayjs().add(1, 'day').toISOString()),
            Cell: ({ cell }) => num(String(cell.getValue<number>()))
        },
        { accessorKey: 'tariff_name', header: t('companies.tariff') },
        {
            accessorKey: 'price',
            header: t('companies.price_month'),
            enableColumnFilter: false,
            Cell: ({ cell }) => <Money digits={2} value={cell.getValue<number>()} />
        },
        {
            id: 'traffic',
            header: t('tariffs.traffic_limit'),
            enableColumnFilter: false,
            accessorFn: (r) => r.traffic_limit_bytes,
            Cell: ({ row }) => num(`${row.original.traffic_limit_bytes ? fmtBytes(row.original.traffic_limit_bytes) : '∞'} · ${strategyLabel(row.original.traffic_strategy)}`)
        },
        {
            id: 'hwid',
            header: t('tariffs.hwid'),
            size: 90,
            enableColumnFilter: false,
            accessorFn: (r) => r.hwid_limit ?? 0,
            Cell: ({ row }) => num(row.original.hwid_limit == null ? '—' : String(row.original.hwid_limit))
        },
        {
            id: 'overrides',
            header: t('companies.overrides'),
            enableColumnFilter: false,
            accessorFn: (r) => r.overrides.join(','),
            Cell: ({ row }) => (
                <Group gap={4} wrap="wrap">
                    {row.original.overrides.length ? <OverrideBadges fields={row.original.overrides} /> : <Text c="dimmed" size="sm">{t('companies.follows_tariff_short')}</Text>}
                </Group>
            )
        }
    ]
}

// SegmentHistory is a billing history: one row per stretch of days, so a
// price or tariff change shows as its own row. Statements are built from
// these rows.
export function SegmentHistory({ segments, storageKey = 'member-segments', title, description }: { segments: MemberSegment[]; storageKey?: string; title?: string; description?: string }) {
    const { t } = useTranslation()
    const columns = useMemo(() => segmentColumns(t as (k: string) => string), [t])
    return (
        <DataTableCard
            compact
            columns={columns}
            data={segments}
            description={description ?? t('companies.history_hint')}
            icon={<IconHistory size={24} />}
            initialState={{ sorting: [{ id: 'period', desc: true }] }}
            storageKey={storageKey}
            title={title ?? t('companies.history')}
        />
    )
}
