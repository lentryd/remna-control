import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { ActionIcon, Alert, Anchor, Badge, Box, Button, Checkbox, FileButton, Group, NumberInput, SimpleGrid, Stack, Text, ThemeIcon, Tooltip } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState, useCallback } from 'react'
import { IconAlertTriangle, IconArchive, IconCamera, IconDeviceFloppy, IconDownload, IconPlus, IconRotate, IconTrash, IconUpload } from '@tabler/icons-react'

import { api } from '@/api/client'
import { useApiMutation, useInvalidateAll, useSettings } from '@/api/hooks'
import { fmtBytes, fmtDateTime, fromNow } from '@/components/format'
import { notifyError, notifyOk } from '@/components/notify'
import { confirmDanger, openModal } from '@/modals/open'
import { FormFooter } from '@shared/ui/forms/form-section'
import { DataTableCard } from '@shared/ui/table'
import { SettingsGroup, SettingsRow } from '@shared/ui/settings-row'
import { StatStrip } from '@shared/ui/stat-strip'
import { useTranslation } from 'react-i18next'

interface BackupCategory {
    key: string
    title: string
    description: string
    tables: string[]
    depends_on?: string[]
}

interface Manifest {
    app_version: string
    created_at: string
    categories: string[]
    tables: Record<string, number>
}

interface Inspected {
    manifest: Manifest
    available: string[]
}

interface ImportResult {
    report: { tables: Record<string, number>; skipped: string[] }
    snapshot: string
}

const CATEGORY_KEYS = ['customers', 'tariffs', 'subscriptions', 'payments', 'expenses', 'settings', 'stats', 'audit'] as const

// useBackupCategories loads the categories with titles in the UI language
// (the server's English ones are the fallback for unknown keys).
function useBackupCategories() {
    const { t } = useTranslation()
    const select = useCallback(
        (d: { categories: BackupCategory[]; counts: Record<string, number> }) => ({
            ...d,
            categories: d.categories.map((c) =>
                (CATEGORY_KEYS as readonly string[]).includes(c.key)
                    ? {
                          ...c,
                          title: t(`backup.cat.${c.key as (typeof CATEGORY_KEYS)[number]}.title`),
                          description: t(`backup.cat.${c.key as (typeof CATEGORY_KEYS)[number]}.description`)
                      }
                    : c
            )
        }),
        [t]
    )
    return useQuery({
        queryKey: ['backup', 'categories'],
        queryFn: () => api.get<{ categories: BackupCategory[]; counts: Record<string, number> }>('backup/categories'),
        select
    })
}

const rowsOf = (c: BackupCategory, tables: Record<string, number>) => c.tables.reduce((n, t) => n + (tables[t] ?? 0), 0)

// CategoryPicker is a checkbox list of backup categories with row counts.
function CategoryPicker({
    categories,
    counts,
    value,
    onChange
}: {
    categories: BackupCategory[]
    counts: (c: BackupCategory) => number
    value: string[]
    onChange: (v: string[]) => void
}) {
    const { t } = useTranslation()
    const allKeys = categories.map((c) => c.key)
    const titleOf = (key: string) => categories.find((c) => c.key === key)?.title ?? key
    return (
        <Stack gap="xs">
            <Group gap="xs">
                <Anchor component="button" size="sm" onClick={() => onChange(allKeys)}>
                    {t('backup.select_all')}
                </Anchor>
                <Text c="dimmed" size="sm">
                    ·
                </Text>
                <Anchor component="button" size="sm" onClick={() => onChange([])}>
                    {t('backup.select_none')}
                </Anchor>
            </Group>
            <Checkbox.Group value={value} onChange={onChange}>
                <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="sm">
                    {categories.map((c) => {
                        const missing = value.includes(c.key) ? (c.depends_on ?? []).filter((d) => !value.includes(d)) : []
                        return (
                            <Checkbox
                                key={c.key}
                                value={c.key}
                                label={
                                    <Group gap={6} wrap="nowrap">
                                        <span>{c.title}</span>
                                        <Badge color="gray" size="xs" variant="soft">
                                            {counts(c)}
                                        </Badge>
                                    </Group>
                                }
                                description={
                                    <>
                                        {c.description}
                                        {missing.length > 0 && (
                                            <Text c="yellow" component="span" display="block" size="xs">
                                                {t('backup.references', { list: missing.map(titleOf).join(', ') })}
                                            </Text>
                                        )}
                                    </>
                                }
                            />
                        )
                    })}
                </SimpleGrid>
            </Checkbox.Group>
        </Stack>
    )
}

// Pane is one half of the transfer card: a titled column of controls.
function Pane({ icon: Icon, color, title, description, children }: { icon: React.ComponentType<{ size: number }>; color: string; title: string; description: string; children: React.ReactNode }) {
    return (
        <Stack className="backup-pane" gap="md">
            <Group align="flex-start" gap="sm" wrap="nowrap">
                <ThemeIcon color={color} radius="md" size={32}>
                    <Icon size={17} />
                </ThemeIcon>
                <Box miw={0}>
                    <Text fw={600} size="sm">
                        {title}
                    </Text>
                    <Text c="dimmed" size="xs">
                        {description}
                    </Text>
                </Box>
            </Group>
            {children}
        </Stack>
    )
}

function ExportCard({ categories, counts }: { categories: BackupCategory[]; counts: Record<string, number> }) {
    const { t } = useTranslation()
    const [selected, setSelected] = useState(() => categories.map((c) => c.key))
    const [busy, setBusy] = useState(false)

    const run = async () => {
        setBusy(true)
        try {
            await api.download(`backup/export?categories=${selected.join(',')}`, 'vpn-control-backup.zip')
        } catch (e) {
            notifyError(e)
        } finally {
            setBusy(false)
        }
    }

    return (
        <Pane color="teal" description={t('backup.export_hint')} icon={IconDownload} title={t('backup.export')}>
            <CategoryPicker categories={categories} counts={(c) => counts[c.key] ?? 0} value={selected} onChange={setSelected} />
            <Group justify="flex-end" mt="auto">
                    <Button
                        disabled={selected.length === 0}
                        leftSection={<IconArchive size={16} />}
                        loading={busy}
                        variant="filled"
                        onClick={run}
                    >
                        {t('backup.download_archive')}
                    </Button>
            </Group>
        </Pane>
    )
}

function ImportCard({ categories }: { categories: BackupCategory[] }) {
    const { t } = useTranslation()
    const [file, setFile] = useState<File | null>(null)
    const [info, setInfo] = useState<Inspected | null>(null)
    const [selected, setSelected] = useState<string[]>([])
    const [inspecting, setInspecting] = useState(false)

    const importMut = useApiMutation((v: { file: File; categories: string[] }) => {
        const form = new FormData()
        form.append('file', v.file)
        form.append('categories', v.categories.join(','))
        return api.upload<ImportResult>('backup/import', form)
    })

    const pick = async (f: File | null) => {
        setFile(f)
        setInfo(null)
        if (!f) return
        setInspecting(true)
        try {
            const form = new FormData()
            form.append('file', f)
            const res = await api.upload<Inspected>('backup/inspect', form)
            setInfo(res)
            setSelected(res.available)
        } catch (e) {
            setFile(null)
            notifyError(e)
        } finally {
            setInspecting(false)
        }
    }

    const available = categories.filter((c) => info?.available.includes(c.key))
    const titles = available.filter((c) => selected.includes(c.key)).map((c) => c.title)

    const run = () => {
        if (!file) return
        confirmDanger(
            t('backup.import_title'),
            <Text size="sm">{t('backup.import_confirm', { list: titles.join(', ') })}</Text>,
            () =>
                importMut.mutate(
                    { file, categories: selected },
                    {
                        onSuccess: (res) => {
                            const rows = Object.values(res.report.tables).reduce((a, b) => a + b, 0)
                            notifyOk(t('backup.imported', { rows, snapshot: res.snapshot }))
                            setFile(null)
                            setInfo(null)
                        },
                        onError: (e) => notifyError(e)
                    }
                ),
            t('backup.import_action')
        )
    }

    return (
        <Pane color="orange" description={t('backup.import_hint')} icon={IconUpload} title={t('backup.import')}>
                <Stack>
                    <Group>
                        <FileButton accept=".zip,application/zip" onChange={pick}>
                            {(props) => (
                                <Button {...props} leftSection={<IconArchive size={16} />} loading={inspecting} variant="default">
                                    {t('backup.choose_archive')}
                                </Button>
                            )}
                        </FileButton>
                        {file && (
                            <Text c="dimmed" size="sm" truncate maw={320}>
                                {file.name}
                            </Text>
                        )}
                    </Group>
                    {info && (
                        <>
                            <Text c="dimmed" size="sm">
                                {t('backup.created_at', { date: fmtDateTime(info.manifest.created_at) })}
                                {info.manifest.app_version && t('backup.version', { version: info.manifest.app_version })}
                            </Text>
                            <CategoryPicker
                                categories={available}
                                counts={(c) => rowsOf(c, info.manifest.tables)}
                                value={selected}
                                onChange={setSelected}
                            />
                            <Alert color="yellow" icon={<IconAlertTriangle size="1em" />}>
                                {t('backup.import_warning')}
                            </Alert>
                        </>
                    )}
                </Stack>
                <Group justify="flex-end" mt="auto">
                    <Button
                        color="orange"
                        disabled={!info || selected.length === 0}
                        leftSection={<IconUpload size={16} />}
                        loading={importMut.isPending}
                        variant="light"
                        onClick={run}
                    >
                        {t('backup.import_action')}
                    </Button>
                </Group>
        </Pane>
    )
}

interface Snapshot {
    name: string
    kind: 'auto' | 'manual' | 'pre-import'
    size: number
    created_at: string
    app_version: string
    categories: string[]
}

const kindColor: Record<Snapshot['kind'], string> = { auto: 'brand', manual: 'teal', 'pre-import': 'orange' }

const useSnapshots = () =>
    useQuery({ queryKey: ['backup', 'snapshots'], queryFn: () => api.get<{ snapshots: Snapshot[] }>('backup/snapshots') })

// SnapshotsCard lists the full backups kept on the server (scheduled,
// manual and pre-import ones) with download, restore and delete.
function SnapshotsCard({ categories }: { categories: BackupCategory[] }) {
    const { t } = useTranslation()
    const q = useSnapshots()
    const invalidate = useInvalidateAll()
    const create = useApiMutation(() => api.post<Snapshot>('backup/snapshots'))
    const remove = (sn: Snapshot) =>
        confirmDanger(t('backup.delete_snapshot', { name: sn.name }), t('backup.delete_snapshot_hint'), async () => {
            try {
                await api.del(`backup/snapshots/${sn.name}`)
                await invalidate()
            } catch (e) {
                notifyError(e)
            }
        })
    const columns = useMemo<MRT_ColumnDef<Snapshot>[]>(
        () => [
            { accessorKey: 'created_at', header: t('backup.col_created'), Cell: ({ cell }) => fmtDateTime(cell.getValue<string>()) },
            {
                accessorKey: 'kind',
                header: t('backup.col_kind'),
                filterVariant: 'multi-select',
                Cell: ({ row }) => (
                    <Badge color={kindColor[row.original.kind]} variant="soft">
                        {t(`backup.kind.${row.original.kind}`)}
                    </Badge>
                )
            },
            { accessorKey: 'size', header: t('backup.col_size'), enableColumnFilter: false, Cell: ({ cell }) => fmtBytes(cell.getValue<number>()) },
            {
                id: 'categories',
                header: t('backup.col_data'),
                enableSorting: false,
                accessorFn: (r) => r.categories?.length ?? 0,
                Cell: ({ row }) =>
                    (row.original.categories?.length ?? 0) === categories.length ? (
                        <Text size="sm">{t('backup.everything')}</Text>
                    ) : (
                        <Text size="sm">
                            {(row.original.categories ?? []).map((k) => categories.find((c) => c.key === k)?.title ?? k).join(', ')}
                        </Text>
                    )
            }
        ],
        [categories, t]
    )
    return (
        <DataTableCard
            actions={
                <Button
                    leftSection={<IconPlus size={14} />}
                    loading={create.isPending}
                    onClick={() => create.mutate(undefined, { onSuccess: (sn) => notifyOk(t('backup.snapshot_created', { name: sn.name })), onError: (e) => notifyError(e) })}
                    size="xs"
                    variant="default"
                >
                    {t('backup.snapshot')}
                </Button>
            }
            columns={columns}
            data={q.data?.snapshots ?? []}
            description={t('backup.snapshots_hint')}
            enableRowActions
            icon={<IconCamera size={24} />}
            initialState={{ sorting: [{ id: 'created_at', desc: true }] }}
            renderRowActions={({ row }) => (
                <Group gap={4} wrap="nowrap">
                    <Tooltip label={t('backup.download')}>
                        <ActionIcon
                            color="gray"
                            onClick={() => api.download(`backup/snapshots/${row.original.name}`, row.original.name).catch(notifyError)}
                            variant="subtle"
                        >
                            <IconDownload size={16} />
                        </ActionIcon>
                    </Tooltip>
                    <Tooltip label={t('backup.restore')}>
                        <ActionIcon
                            color="orange"
                            onClick={() =>
                                openModal(
                                    { icon: IconRotate, color: 'orange', title: t('backup.restore'), subtitle: row.original.name },
                                    (close) => <RestoreSnapshotForm categories={categories} name={row.original.name} onDone={close} />,
                                    'lg'
                                )
                            }
                            variant="subtle"
                        >
                            <IconRotate size={16} />
                        </ActionIcon>
                    </Tooltip>
                    <Tooltip label={t('common.delete')}>
                        <ActionIcon color="red" onClick={() => remove(row.original)} variant="subtle">
                            <IconTrash size={16} />
                        </ActionIcon>
                    </Tooltip>
                </Group>
            )}
            displayColumnDefOptions={{ 'mrt-row-actions': { header: '', size: 120 } }}
            state={{ isLoading: q.isPending }}
            storageKey="snapshots"
            title={t('backup.snapshots')}
        />
    )
}

function RestoreSnapshotForm({ name, categories, onDone }: { name: string; categories: BackupCategory[]; onDone: () => void }) {
    const { t } = useTranslation()
    const info = useQuery({ queryKey: ['backup', 'inspect', name], queryFn: () => api.get<Inspected>(`backup/snapshots/${name}/inspect`) })
    const [selected, setSelected] = useState<string[]>([])
    useEffect(() => {
        if (info.data) setSelected(info.data.available)
    }, [info.data])
    const m = useApiMutation(() => api.post<ImportResult>(`backup/snapshots/${name}/restore`, { categories: selected }))
    const available = categories.filter((c) => info.data?.available.includes(c.key))
    return (
        <Stack>
            {info.data && (
                <CategoryPicker
                    categories={available}
                    counts={(c) => rowsOf(c, info.data.manifest.tables)}
                    value={selected}
                    onChange={setSelected}
                />
            )}
            <Alert color="yellow" icon={<IconAlertTriangle size="1em" />}>
                {t('backup.restore_warning')}
            </Alert>
            <FormFooter
                disabled={!selected.length}
                inline
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    m.mutate(undefined, {
                        onSuccess: (res) => {
                            notifyOk(t('backup.restored', { snapshot: res.snapshot }))
                            onDone()
                        },
                        onError: (e) => notifyError(e)
                    })
                }
                submitIcon={<IconRotate size={16} />}
                submitLabel={t('backup.restore')}
            />
        </Stack>
    )
}

// ScheduleGroup sets how often automatic snapshots are taken and kept.
function ScheduleGroup() {
    const { t } = useTranslation()
    const settings = useSettings()
    const [hours, setHours] = useState<number | string>('')
    const [keep, setKeep] = useState<number | string>('')
    useEffect(() => {
        if (!settings.data) return
        setHours(Number(settings.data.snapshot_interval_hours))
        setKeep(Number(settings.data.snapshot_keep))
    }, [settings.data])
    const dirty = !!settings.data && (String(hours) !== String(Number(settings.data.snapshot_interval_hours)) || String(keep) !== String(Number(settings.data.snapshot_keep)))
    const save = useApiMutation(() => api.put('settings', { snapshot_interval_hours: String(hours), snapshot_keep: String(keep) }))
    return (
        <SettingsGroup
            actions={
                <Button
                    disabled={!dirty}
                    leftSection={<IconDeviceFloppy size={16} />}
                    loading={save.isPending}
                    onClick={() => save.mutate(undefined, { onSuccess: () => notifyOk(t('common.saved')), onError: (e) => notifyError(e) })}
                    size="xs"
                    variant={dirty ? 'filled' : 'default'}
                >
                    {t('common.save')}
                </Button>
            }
            description={t('backup.schedule_hint')}
            title={t('backup.schedule')}
        >
            <SettingsRow description={t('backup.every_hours_hint')} label={t('backup.every_hours')}>
                <NumberInput min={0} onChange={setHours} suffix={t('backup.hours_suffix')} value={hours} />
            </SettingsRow>
            <SettingsRow description={t('backup.keep_hint')} label={t('backup.keep')}>
                <NumberInput min={1} onChange={setKeep} value={keep} />
            </SettingsRow>
        </SettingsGroup>
    )
}

// BackupSummary: when the last snapshot was taken, how many are kept and
// what the schedule is.
function BackupSummary() {
    const { t } = useTranslation()
    const q = useSnapshots()
    const settings = useSettings()
    const list = q.data?.snapshots ?? []
    const last = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
    const hours = Number(settings.data?.snapshot_interval_hours ?? 0)
    return (
        <StatStrip
            items={[
                { label: t('backup.last_snapshot'), value: last ? fromNow(last.created_at) : t('backup.none_yet'), hint: last ? fmtDateTime(last.created_at) : undefined },
                { label: t('backup.count'), value: list.length },
                { label: t('backup.total_size'), value: fmtBytes(list.reduce((n, sn) => n + sn.size, 0)) },
                {
                    label: t('backup.schedule_state'),
                    value: hours > 0 ? t('backup.schedule_on', { hours }) : t('backup.schedule_off'),
                    hint: hours > 0 ? t('backup.keep_n', { keep: settings.data?.snapshot_keep }) : undefined,
                    color: hours > 0 ? undefined : 'yellow'
                }
            ]}
        />
    )
}

export function BackupPanel() {
    const { t } = useTranslation()
    const q = useBackupCategories()
    if (!q.data) return null
    return (
        <Stack gap="lg">
            <BackupSummary />
            <SnapshotsCard categories={q.data.categories} />
            <ScheduleGroup />
            <SettingsGroup description={t('backup.transfer_hint')} title={t('backup.transfer')}>
                <div className="backup-transfer">
                    <ExportCard categories={q.data.categories} counts={q.data.counts} />
                    <ImportCard categories={q.data.categories} />
                </div>
            </SettingsGroup>
        </Stack>
    )
}
