import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { ActionIcon, Alert, Badge, Box, Button, Group, Menu, NumberInput, SegmentedControl, Skeleton, Stack, Tabs, Text, Textarea, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import {
    IconArchive,
    IconBuilding,
    IconCalendarEvent,
    IconChevronDown,
    IconCreditCard,
    IconFileImport,
    IconKey,
    IconLock,
    IconLockOpen,
    IconPlayerPlay,
    IconPlus,
    IconReceipt2,
    IconScale,
    IconTag,
    IconTrash,
    IconUserPlus,
    IconUsers,
    IconUserCheck,
    IconAlertTriangle,
    IconWallet
} from '@tabler/icons-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router'

import { api } from '@/api/client'
import { useApiMutation, useCompany, useCompanyMembers, useCompanyStatements, useCustomer } from '@/api/hooks'
import type { Company, CompanyMember, CompanyStatement, LedgerEntry } from '@/api/types'
import { ApiTokenItem } from '@/components/api-tokens/api-token-item'
import { openCreateApiToken } from '@/components/api-tokens/api-tokens-card'
import { useApiTokens } from '@/components/api-tokens/hooks'
import { StatusPill } from '@/components/badges'
import { fmtDate, fmtDateTime, fmtMoney } from '@/components/format'
import { ledgerColumns } from '@/components/ledger'
import { errorText, notifyError, notifyOk } from '@/components/notify'
import { useMemberActions } from '@/components/MemberActions'
import { paymentColumns, paymentTableProps } from '@/components/PaymentTable'
import { OverrideBadges } from '@/components/segments'
import { BackLink, Money, PageHeader } from '@/components/ui'
import {
    confirmAction,
    openAddMember,
    openImportMembers,
    openMemberOverrides,
    openTariffChange
} from '@/modals/CompanyModals'
import { openAdjustModal } from '@/modals/CustomerModals'
import { confirmDanger, openModal } from '@/modals/open'
import { openPaymentModal } from '@/modals/PaymentModal'
import { openMemberModal } from '@/modals/MemberModal'
import { openStatementModal } from '@/modals/StatementModal'
import { EntityAdd, EntityGrid } from '@shared/ui/entity-card'
import { FormColumns, FormSection } from '@shared/ui/forms/form-section'
import { ExternalSquadSelect } from '@shared/ui/infra/squad'
import { LoadingScreen } from '@shared/ui/loading-screen'
import { Page } from '@shared/ui/page'
import { StatStrip } from '@shared/ui/stat-strip'
import { DataTableCard } from '@shared/ui/table'

// debtSince is the date the balance last went below zero and stayed there,
// found by walking the ledger back from the current balance.
function debtSince(balance: number, ledger: LedgerEntry[] | null | undefined): string | null {
    if (balance >= 0 || !ledger?.length) return null
    let bal = balance
    let since: string | null = null
    for (const e of [...ledger].sort((a, b) => +new Date(b.date) - +new Date(a.date))) {
        if (bal >= 0) break
        since = e.date
        bal -= e.amount
    }
    return since
}

export function CompanyPage() {
    const { t } = useTranslation()
    const id = Number(useParams().id)
    const { data: company, isPending, error } = useCompany(id)
    if (isPending) return <LoadingScreen height="60vh" />
    if (error || !company) return <Alert color="red">{error ? errorText(error) : t('errors.company.not_found')}</Alert>
    return <CompanyCard company={company} />
}

function CompanyCard({ company }: { company: Company }) {
    const { t } = useTranslation()
    const account = useCustomer(company.customer_id)
    const since = debtSince(company.balance, account.data?.ledger)

    return (
        <Page title={company.name}>
            <PageHeader
                eyebrow={<BackLink to="/companies">{t('menu.companies')}</BackLink>}
                title={company.name}
                badges={
                    company.archived && (
                        <Badge color="gray" size="lg">
                            {t('customer.archived')}
                        </Badge>
                    )
                }
                description={
                    <Group gap={6} component="span">
                        <span>
                            {t('companies.account')}:{' '}
                            <Text component={Link} inherit c="brand" to={`/customers/${company.customer_id}`}>
                                {account.data?.name ?? `#${company.customer_id}`}
                            </Text>
                        </span>
                        {company.notes && <span>· {company.notes}</span>}
                    </Group>
                }
                actions={
                    !company.archived && (
                        <>
                            <Button leftSection={<IconCreditCard size={16} />} onClick={() => openPaymentModal({ customerId: company.customer_id, name: company.name })} variant="filled">
                                {t('customer.record_payment')}
                            </Button>
                            <Menu position="bottom-end">
                                <Menu.Target>
                                    <Button rightSection={<IconChevronDown size={14} />}>{t('customer.more')}</Button>
                                </Menu.Target>
                                <Menu.Dropdown>
                                    <Menu.Item leftSection={<IconUserPlus size={16} />} onClick={() => openAddMember(company)}>
                                        {t('companies.add_member')}
                                    </Menu.Item>
                                    <Menu.Item leftSection={<IconFileImport size={16} />} onClick={() => openImportMembers(company)}>
                                        {t('companies.import')}
                                    </Menu.Item>
                                    <Menu.Item leftSection={<IconScale size={16} />} onClick={() => openAdjustModal({ id: company.customer_id, name: company.name })}>
                                        {t('customer.adjust_balance')}
                                    </Menu.Item>
                                </Menu.Dropdown>
                            </Menu>
                        </>
                    )
                }
            />

            <StatStrip
                items={[
                    {
                        label: t('dashboard.col_balance'),
                        value: <Money value={company.balance} signed digits={2} />,
                        color: company.balance < 0 ? 'red' : undefined,
                        hint: company.balance < 0 ? (since ? t('companies.in_debt_since', { date: fmtDate(since) }) : t('companies.in_debt')) : undefined,
                        icon: IconWallet
                    },
                    {
                        label: t('companies.next_report'),
                        value: fmtDate(company.period_end),
                        hint: t('companies.accrued_forecast', { accrued: fmtMoney(company.accrued, 2), forecast: fmtMoney(company.forecast, 2) }),
                        icon: IconCalendarEvent
                    },
                    {
                        label: t('companies.members'),
                        value: company.members_count,
                        hint: `${t('companies.protected_count', { count: company.protected_count })} · ${company.tariff_name}`,
                        icon: IconUsers
                    }
                ]}
                mb="lg"
            />

            <Tabs defaultValue="members" keepMounted={false}>
                <Tabs.List mb="md">
                    <Tabs.Tab value="members">{t('companies.tab_members')}</Tabs.Tab>
                    <Tabs.Tab value="statements">{t('companies.tab_statements')}</Tabs.Tab>
                    <Tabs.Tab value="payments">{t('menu.payments')}</Tabs.Tab>
                    <Tabs.Tab value="tokens">{t('settings.tokens')}</Tabs.Tab>
                    <Tabs.Tab value="settings">{t('menu.settings')}</Tabs.Tab>
                </Tabs.List>
                <Tabs.Panel value="members">
                    <MembersTab company={company} />
                </Tabs.Panel>
                <Tabs.Panel value="statements">
                    <StatementsTab company={company} />
                </Tabs.Panel>
                <Tabs.Panel value="payments">
                    <PaymentsTab company={company} />
                </Tabs.Panel>
                <Tabs.Panel value="tokens">
                    <TokensTab company={company} />
                </Tabs.Panel>
                <Tabs.Panel value="settings">
                    <SettingsTab company={company} />
                </Tabs.Panel>
            </Tabs>
        </Page>
    )
}

/* ---------------------------------- employees ---------------------------------- */

function MembersTab({ company }: { company: Company }) {
    const { t } = useTranslation()
    const { data, isFetching } = useCompanyMembers(company.id)
    const [scope, setScope] = useState('active')
    const rows = useMemo(() => (data ?? []).filter((m) => (scope === 'all' ? true : scope === 'removed' ? !m.active : m.active)), [data, scope])
    const actions = useMemberActions(company)
    const { remove, toggleProtect, restore } = actions

    const columns = useMemo<MRT_ColumnDef<CompanyMember>[]>(
        () => [
            {
                accessorKey: 'username',
                header: t('companies.username'),
                Cell: ({ row }) => (
                    <Group gap={6} wrap="nowrap" pl={10}>
                        <Box>
                            <Text fw={500} size="sm">
                                {row.original.username}
                            </Text>
                            {row.original.label && (
                                <Text c="dimmed" size="xs">
                                    {row.original.label}
                                </Text>
                            )}
                        </Box>
                    </Group>
                )
            },
            {
                id: 'tariff',
                header: t('companies.tariff'),
                accessorFn: (r) => r.tariff_name,
                Cell: ({ row }) => (
                    <Group gap={4} wrap="wrap">
                        <Text size="sm">{row.original.tariff_name}</Text>
                        {row.original.tariff_pinned && (
                            <Badge color="orange" size="xs" variant="soft">
                                {t('companies.pinned')}
                            </Badge>
                        )}
                        <OverrideBadges fields={row.original.overrides} />
                    </Group>
                )
            },
            { accessorKey: 'price', header: t('companies.price_month'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} digits={2} /> },
            {
                id: 'added',
                header: t('companies.added'),
                sortingFn: 'datetime',
                accessorFn: (r) => new Date(r.added_at),
                enableColumnFilter: false,
                Cell: ({ row }) => <Text size="sm">{fmtDate(row.original.added_at)}</Text>
            },
            {
                id: 'status',
                header: t('companies.status'),
                accessorFn: (r) => (r.active ? r.panel_status : 'removed'),
                Cell: ({ row }) => {
                    const m = row.original
                    return (
                        <Group gap={6} wrap="nowrap">
                            {m.active ? (
                                <StatusPill status={m.panel_status || 'ACTIVE'} size="sm" />
                            ) : (
                                <Badge color="gray" size="sm" variant="soft">
                                    {t('companies.removed_on', { date: fmtDate(m.removed_at) })}
                                </Badge>
                            )}
                            {m.protected && (
                                <Tooltip label={t('companies.protected')} withArrow>
                                    <IconLock size={14} />
                                </Tooltip>
                            )}
                            {m.panel_deleted && (
                                <Tooltip label={t('companies.panel_deleted')} withArrow>
                                    <IconAlertTriangle color="var(--mantine-color-orange-5)" size={16} />
                                </Tooltip>
                            )}
                        </Group>
                    )
                }
            }
        ],
        [t]
    )

    return (
        <DataTableCard
            storageKey="company-members"
            icon={<IconUsers size={24} />}
            title={t('companies.tab_members')}
            actions={
                <Group gap="xs">
                    <SegmentedControl
                        size="xs"
                        value={scope}
                        onChange={setScope}
                        data={[
                            { value: 'active', label: t('customers.scope_active') },
                            { value: 'removed', label: t('companies.scope_removed') },
                            { value: 'all', label: t('common.all') }
                        ]}
                    />
                    {!company.archived && (
                        <>
                            <Button leftSection={<IconFileImport size={14} />} onClick={() => openImportMembers(company)} size="compact-md" variant="default">
                                {t('companies.import')}
                            </Button>
                            <Button leftSection={<IconUserPlus size={14} />} onClick={() => openAddMember(company)} size="compact-md" variant="filled">
                                {t('companies.add_member')}
                            </Button>
                        </>
                    )}
                </Group>
            }
            columns={columns}
            data={rows}
            state={{ showProgressBars: isFetching || actions.busy, isLoading: !data }}
            initialState={{ sorting: [{ id: 'username', desc: false }] }}
            enableRowActions
            onRowClick={(m) => openMemberModal(company, m)}
            displayColumnDefOptions={{ 'mrt-row-actions': { header: '', size: 140 } }}
            renderRowActions={({ row }) => {
                const m = row.original
                if (!m.active) {
                    return (
                        <Button disabled={company.archived} leftSection={<IconUserCheck size={14} />} onClick={() => restore(m)} size="compact-sm" variant="default">
                            {t('companies.restore')}
                        </Button>
                    )
                }
                return (
                    <Group gap={6} wrap="nowrap">
                        <Tooltip label={t('companies.member_tariff')} withArrow>
                            <ActionIcon onClick={() => openMemberOverrides(company, m)} size="md" variant="default">
                                <IconTag size={14} />
                            </ActionIcon>
                        </Tooltip>
                        <Tooltip label={m.protected ? t('companies.unprotect') : t('companies.protect')} withArrow>
                            <ActionIcon color={m.protected ? 'brand' : undefined} onClick={() => toggleProtect(m)} size="md" variant={m.protected ? 'light' : 'default'}>
                                {m.protected ? <IconLock size={14} /> : <IconLockOpen size={14} />}
                            </ActionIcon>
                        </Tooltip>
                        <Tooltip label={m.protected ? t('companies.protected_cant_remove') : t('companies.remove')} withArrow>
                            <ActionIcon color="red" disabled={m.protected} onClick={() => remove(m)} size="md" variant="default">
                                <IconTrash size={14} />
                            </ActionIcon>
                        </Tooltip>
                    </Group>
                )
            }}
        />
    )
}

/* ---------------------------------- statements ---------------------------------- */

function StatementsTab({ company }: { company: Company }) {
    const { t } = useTranslation()
    const { data, isFetching } = useCompanyStatements(company.id)
    const columns = useMemo<MRT_ColumnDef<CompanyStatement>[]>(
        () => [
            {
                id: 'period',
                header: t('companies.period'),
                sortingFn: 'datetime',
                accessorFn: (r) => new Date(r.period_from),
                Cell: ({ row }) => {
                    const to = new Date(row.original.period_to)
                    to.setDate(to.getDate() - 1)
                    return (
                        <Text className="num" fw={500} size="sm">
                            {fmtDate(row.original.period_from)} → {fmtDate(to.toISOString())}
                        </Text>
                    )
                }
            },
            { accessorKey: 'total', header: t('companies.total'), enableColumnFilter: false, Cell: ({ cell }) => <Money value={cell.getValue<number>()} digits={2} /> },
            {
                accessorKey: 'created_at',
                header: t('companies.generated'),
                sortingFn: 'datetime',
                accessorFn: (r) => new Date(r.created_at),
                enableColumnFilter: false,
                Cell: ({ row }) => <Text size="sm">{fmtDateTime(row.original.created_at)}</Text>
            },
            { accessorKey: 'generated_by', header: t('customer.col_actor') }
        ],
        [t]
    )
    const close = useApiMutation(() => api.post<{ posted: number }>(`companies/${company.id}/close-period`))
    return (
        <DataTableCard
            storageKey="company-statements"
            icon={<IconReceipt2 size={24} />}
            title={t('companies.tab_statements')}
            description={t('companies.statements_hint')}
            actions={
                !company.archived && (
                    <Button
                        leftSection={<IconPlayerPlay size={14} />}
                        loading={close.isPending}
                        onClick={() =>
                            confirmAction(
                                t('companies.close_period'),
                                t('companies.close_period_hint'),
                                () => close.mutate(undefined, { onSuccess: (r) => notifyOk(t('companies.closed', { count: r.posted })), onError: (e) => notifyError(e) }),
                                t('companies.close_period_run')
                            )
                        }
                        size="compact-md"
                        variant="default"
                    >
                        {t('companies.close_period')}
                    </Button>
                )
            }
            columns={columns}
            data={data ?? []}
            state={{ showProgressBars: isFetching, isLoading: !data }}
            initialState={{ sorting: [{ id: 'period', desc: true }] }}
            onRowClick={(r) => openStatementModal(company.id, r.id)}
        />
    )
}

/* ---------------------------------- payments ---------------------------------- */

function PaymentsTab({ company }: { company: Company }) {
    const { t } = useTranslation()
    const { data } = useCustomer(company.customer_id)
    const cols = useMemo(() => ({ payments: paymentColumns(false), ledger: ledgerColumns((sid) => openStatementModal(company.id, sid)) }), [t, company.id])
    return (
        <Stack gap="lg">
            <DataTableCard
                compact
                storageKey="company-payments"
                icon={<IconCreditCard size={24} />}
                title={t('menu.payments')}
                actions={
                    !company.archived && (
                        <Button leftSection={<IconPlus size={14} />} onClick={() => openPaymentModal({ customerId: company.customer_id, name: company.name })} size="compact-md" variant="filled">
                            {t('customer.record_payment')}
                        </Button>
                    )
                }
                {...paymentTableProps}
                columns={cols.payments}
                data={(data?.payments ?? []).map((p) => ({ ...p, customer_name: company.name }))}
                state={{ isLoading: !data }}
            />
            <DataTableCard
                compact
                storageKey="company-ledger"
                icon={<IconWallet size={24} />}
                title={t('customer.ledger_title')}
                actions={
                    <Button leftSection={<IconScale size={14} />} onClick={() => openAdjustModal({ id: company.customer_id, name: company.name })} size="compact-md" variant="default">
                        {t('customer.adjust_balance')}
                    </Button>
                }
                columns={cols.ledger}
                data={data?.ledger ?? []}
                state={{ isLoading: !data }}
                initialState={{ sorting: [{ id: 'date', desc: true }] }}
            />
        </Stack>
    )
}

/* ---------------------------------- tokens ---------------------------------- */

function TokensTab({ company }: { company: Company }) {
    const { t } = useTranslation()
    const { data } = useApiTokens()
    const tokens = (data?.tokens ?? []).filter((x) => x.company_id === company.id)
    const create = () => openCreateApiToken(t('tokens.create'), company.id)
    return (
        <Box>
            <Group align="flex-start" justify="space-between" mb="md" wrap="nowrap">
                <Box miw={0}>
                    <Text c="var(--app-text-strong)" fw={600} fz={17} style={{ letterSpacing: '-0.01em' }}>
                        {t('settings.tokens')}
                    </Text>
                    <Text c="dimmed" size="sm">
                        {t('companies.tokens_hint')}
                    </Text>
                </Box>
                {!company.archived && (
                    <Button leftSection={<IconKey size={16} />} onClick={create} variant="filled">
                        {t('tokens.create')}
                    </Button>
                )}
            </Group>
            <EntityGrid min={280}>
                {!data && [0, 1].map((i) => <Skeleton h={200} key={i} radius="lg" />)}
                {tokens.map((token) => (
                    <ApiTokenItem key={token.id} token={token} />
                ))}
                {data && tokens.length === 0 && !company.archived && <EntityAdd label={t('tokens.none_cta')} onClick={create} />}
            </EntityGrid>
        </Box>
    )
}

/* ---------------------------------- settings ---------------------------------- */

function SettingsTab({ company }: { company: Company }) {
    const { t } = useTranslation()
    const form = useForm({
        initialValues: {
            name: company.name,
            notes: company.notes,
            report_day: company.report_day as number | string,
            external_squad_uuid: company.external_squad_uuid
        },
        validate: {
            name: (v) => (v.trim() ? null : t('tariffs.name_required')),
            report_day: (v) => (Number(v) >= 1 && Number(v) <= 28 ? null : t('companies.report_day_hint'))
        }
    })
    useEffect(() => {
        const values = { name: company.name, notes: company.notes, report_day: company.report_day, external_squad_uuid: company.external_squad_uuid }
        form.setValues(values)
        form.resetDirty(values)
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [company.name, company.notes, company.report_day, company.external_squad_uuid])
    const save = useApiMutation((v: typeof form.values) =>
        api.put(`companies/${company.id}`, {
            name: v.name.trim(),
            notes: v.notes,
            tariff_id: company.tariff_id,
            report_day: Number(v.report_day),
            external_squad_uuid: v.external_squad_uuid
        })
    )
    const archive = useApiMutation(() => api.post<{ protected_left_enabled: string[] }>(`companies/${company.id}/archive`))

    const doArchive = () =>
        confirmDanger(
            t('companies.archive'),
            t('companies.archive_hint'),
            () =>
                archive.mutate(undefined, {
                    onSuccess: (r) => {
                        const kept = r?.protected_left_enabled ?? []
                        if (kept.length) {
                            openModal({ icon: IconArchive, color: 'orange', title: t('companies.archived_title'), subtitle: company.name }, (close) => (
                                <Stack gap="md">
                                    <Alert color="orange" icon={<IconLock size={18} />} variant="soft">
                                        {t('companies.protected_left', { count: kept.length })}
                                    </Alert>
                                    <Group gap={6}>
                                        {kept.map((u) => (
                                            <Badge key={u} variant="light">
                                                {u}
                                            </Badge>
                                        ))}
                                    </Group>
                                    <Group justify="flex-end">
                                        <Button onClick={close} variant="default">
                                            {t('common.close')}
                                        </Button>
                                    </Group>
                                </Stack>
                            ))
                        } else notifyOk(t('companies.archived'))
                    },
                    onError: (e) => notifyError(e)
                }),
            t('companies.archive')
        )

    return (
        <FormColumns
            left={
                <FormSection description={t('companies.settings_general_hint')} icon={IconBuilding} title={t('companies.settings_general')}>
                    <form onSubmit={form.onSubmit((v) => save.mutate(v, { onSuccess: () => notifyOk(t('companies.saved')), onError: (e) => notifyError(e) }))}>
                        <Stack gap="md">
                            <TextInput label={t('companies.name')} leftSection={<IconBuilding size={16} />} required {...form.getInputProps('name')} />
                            <NumberInput
                                allowDecimal={false}
                                description={t('companies.report_day_hint')}
                                label={t('companies.report_day')}
                                leftSection={<IconCalendarEvent size={16} />}
                                max={28}
                                min={1}
                                {...form.getInputProps('report_day')}
                            />
                            <ExternalSquadSelect
                                description={t('companies.external_squad_hint')}
                                label={t('companies.external_squad_label')}
                                placeholder={t('companies.external_squad_inherit')}
                                {...form.getInputProps('external_squad_uuid')}
                            />
                            <Textarea autosize label={t('expense_items.notes')} minRows={3} {...form.getInputProps('notes')} />
                            <Group justify="flex-end">
                                <Button disabled={!form.isDirty() || company.archived} loading={save.isPending} type="submit">
                                    {t('common.save')}
                                </Button>
                            </Group>
                        </Stack>
                    </form>
                </FormSection>
            }
            right={
                <>
                    <FormSection description={t('companies.tariff_cascade_hint')} icon={IconTag} title={t('companies.tariff')}>
                        <Group justify="space-between" wrap="wrap">
                            <Text fw={500}>{company.tariff_name}</Text>
                            <Button disabled={company.archived} leftSection={<IconTag size={16} />} onClick={() => openTariffChange(company)} variant="default">
                                {t('companies.change_tariff')}
                            </Button>
                        </Group>
                    </FormSection>
                    <FormSection color="red" description={t('companies.archive_hint')} icon={IconArchive} title={t('common.danger_zone')}>
                        <Button
                            color="red"
                            disabled={company.archived}
                            leftSection={<IconArchive size={16} />}
                            loading={archive.isPending}
                            onClick={doArchive}
                            variant="light"
                        >
                            {t('companies.archive')}
                        </Button>
                    </FormSection>
                </>
            }
        />
    )
}
