import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { Badge, Button, Group, NumberInput, Select, Stack, Text, Transition } from '@mantine/core'
import { IconActivityHeartbeat, IconArchive, IconHistory, IconKey, IconAdjustmentsHorizontal } from '@tabler/icons-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router'

import { api } from '@/api/client'
import { useApiMutation, useAudit, useSettings } from '@/api/hooks'
import type { AuditRow } from '@/api/types'
import { ApiTokensCard } from '@/components/api-tokens/api-tokens-card'
import { BackupPanel } from '@/components/BackupPanel'
import { fmtDateTime } from '@/components/format'
import { JobsPanel } from '@/components/JobsPanel'
import { notifyError, notifyOk } from '@/components/notify'
import { PageHeader } from '@/components/ui'
import { BASE_CURRENCIES, CurrencyIcon } from '@shared/currencies'
import { Page } from '@shared/ui/page'
import { SectionNav } from '@shared/ui/section-nav'
import { SettingsGroup, SettingsRow } from '@shared/ui/settings-row'
import { DataTableCard } from '@shared/ui/table'

import classes from './settings.module.css'

// Keys edited on the parameters section; the rest of settings belong to
// other sections (backups) or are read-only flags.
const EDITABLE = ['base_currency', 'referral_percent', 'expiring_window_days', 'default_fee_percent'] as const

const SECTIONS = ['params', 'backups', 'jobs', 'tokens', 'audit'] as const

const saveBarTransition = {
    in: { opacity: 1, transform: 'translateY(0)' },
    out: { opacity: 0, transform: 'translateY(12px)' },
    transitionProperty: 'opacity, transform'
}
type Section = (typeof SECTIONS)[number]

export function SettingsPage() {
    const { t } = useTranslation()
    const [params, setParams] = useSearchParams()
    const section = (SECTIONS as readonly string[]).includes(params.get('tab') ?? '') ? (params.get('tab') as Section) : 'params'

    return (
        <Page title={t('menu.settings')}>
            <PageHeader description={t('settings.description')} title={t('menu.settings')} />
            <SectionNav
                items={[
                    { value: 'params', label: t('settings.params'), hint: t('settings.nav.params_hint'), icon: IconAdjustmentsHorizontal },
                    { value: 'backups', label: t('settings.backups'), hint: t('settings.nav.backups_hint'), icon: IconArchive },
                    { value: 'jobs', label: t('settings.jobs'), hint: t('settings.nav.jobs_hint'), icon: IconActivityHeartbeat },
                    { value: 'tokens', label: t('settings.tokens'), hint: t('settings.nav.tokens_hint'), icon: IconKey },
                    { value: 'audit', label: t('backup.cat.audit.title'), hint: t('settings.nav.audit_hint'), icon: IconHistory }
                ]}
                onChange={(v) => setParams(v === 'params' ? {} : { tab: v }, { replace: true })}
                value={section}
            >
                {section === 'params' && <ParamsSection />}
                {section === 'backups' && <BackupPanel />}
                {section === 'jobs' && <JobsPanel />}
                {section === 'tokens' && <ApiTokensCard />}
                {section === 'audit' && <AuditSection />}
            </SectionNav>
        </Page>
    )
}

function ParamsSection() {
    const { t } = useTranslation()
    const settings = useSettings()
    const [values, setValues] = useState<Record<string, string>>({})
    useEffect(() => {
        if (settings.data) setValues(settings.data)
    }, [settings.data])
    const save = useApiMutation((v: Record<string, string>) => api.put('settings', v))
    const dirty = !!settings.data && EDITABLE.some((k) => String(values[k] ?? '') !== String(settings.data[k] ?? ''))
    const set = (key: string) => (v: string | number) => setValues((s) => ({ ...s, [key]: String(v) }))
    const locked = values._base_locked === 'true'

    const number = (key: (typeof EDITABLE)[number], suffix: string) => (
        <NumberInput decimalScale={2} min={0} onChange={set(key)} suffix={suffix} value={values[key] ?? ''} />
    )

    return (
        <Stack gap="lg">
            <SettingsGroup description={t('settings.group_money_hint')} title={t('settings.group_money')}>
                <SettingsRow description={locked ? t('settings.base_locked') : t('settings.base_hint')} label={t('settings.base_currency')}>
                    <Select
                        allowDeselect={false}
                        data={BASE_CURRENCIES}
                        disabled={locked}
                        leftSection={<CurrencyIcon currency={values.base_currency} size={16} />}
                        onChange={(v) => v && setValues((s) => ({ ...s, base_currency: v }))}
                        searchable
                        value={values.base_currency ?? null}
                    />
                </SettingsRow>
                <SettingsRow description={t('settings.fields.default_fee_percent.description')} label={t('settings.fields.default_fee_percent.label')}>
                    {number('default_fee_percent', '%')}
                </SettingsRow>
            </SettingsGroup>

            <SettingsGroup description={t('settings.group_referrals_hint')} title={t('settings.group_referrals')}>
                <SettingsRow description={t('settings.fields.referral_percent.description')} label={t('settings.fields.referral_percent.label')}>
                    {number('referral_percent', '%')}
                </SettingsRow>
            </SettingsGroup>

            <SettingsGroup description={t('settings.group_dashboard_hint')} title={t('settings.group_dashboard')}>
                <SettingsRow description={t('settings.fields.expiring_window_days.description')} label={t('settings.fields.expiring_window_days.label')}>
                    {number('expiring_window_days', ` ${t('settings.days_suffix')}`)}
                </SettingsRow>
            </SettingsGroup>

            <Transition duration={160} mounted={dirty} transition={saveBarTransition}>
                {(style) => (
                    <div className={classes.saveBar} style={style}>
                        <Text fw={500} size="sm">
                            {t('settings.unsaved')}
                        </Text>
                        <Group gap="xs">
                            <Button onClick={() => settings.data && setValues(settings.data)} size="sm" variant="default">
                                {t('settings.discard')}
                            </Button>
                            <Button
                                loading={save.isPending}
                                onClick={() =>
                                    save.mutate(Object.fromEntries(EDITABLE.map((k) => [k, values[k] ?? ''])), {
                                        onSuccess: () => notifyOk(t('settings.saved')),
                                        onError: (e) => notifyError(e)
                                    })
                                }
                                size="sm"
                                variant="filled"
                            >
                                {t('common.save')}
                            </Button>
                        </Group>
                    </div>
                )}
            </Transition>
        </Stack>
    )
}

function AuditSection() {
    const { t } = useTranslation()
    const audit = useAudit()
    const columns = useMemo<MRT_ColumnDef<AuditRow>[]>(
        () => [
            {
                id: 'at',
                header: t('customer.col_when'),
                sortingFn: 'datetime',
                accessorFn: (r) => new Date(r.at),
                enableColumnFilter: false,
                Cell: ({ row }) => <Text className="num" size="sm">{fmtDateTime(row.original.at)}</Text>
            },
            { accessorKey: 'actor', header: t('customer.col_actor'), filterVariant: 'multi-select' },
            {
                accessorKey: 'action',
                header: t('customer.col_action'),
                filterVariant: 'multi-select',
                Cell: ({ cell }) => (
                    <Text ff="monospace" size="xs">
                        {cell.getValue<string>()}
                    </Text>
                )
            },
            { id: 'entity', header: t('settings.col_entity'), accessorFn: (r) => `${r.entity} #${r.entity_id}` },
            {
                accessorKey: 'ok',
                header: t('settings.col_result'),
                Cell: ({ row }) =>
                    row.original.ok ? (
                        <Badge color="teal">ok</Badge>
                    ) : (
                        <Badge color="red" title={row.original.error}>
                            {t('common.error')}
                        </Badge>
                    )
            },
            {
                accessorKey: 'payload',
                header: t('backup.col_data'),
                Cell: ({ row }) => (
                    <Text c="dimmed" ff="monospace" maw={420} size="xs" title={row.original.error || row.original.payload} truncate>
                        {row.original.error || row.original.payload}
                    </Text>
                )
            }
        ],
        [t]
    )
    return (
        <DataTableCard
            columns={columns}
            data={audit.data ?? []}
            description={t('settings.audit_hint')}
            icon={<IconHistory />}
            initialState={{ sorting: [{ id: 'at', desc: true }] }}
            state={{ isLoading: !audit.data }}
            storageKey="audit"
            title={t('backup.cat.audit.title')}
        />
    )
}
