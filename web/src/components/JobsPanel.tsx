import type { MRT_ColumnDef } from '@kastov/mantine-react-table-open'
import { ActionIcon, Badge, Box, Button, Code, CopyButton, Group, Loader, SimpleGrid, Stack, Text, Tooltip } from '@mantine/core'
import { IconActivityHeartbeat, IconCheck, IconCopy, IconPlayerPlay } from '@tabler/icons-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { api } from '@/api/client'
import { useApiMutation, useJobRuns, useJobs } from '@/api/hooks'
import type { Job, JobRun } from '@/api/types'
import { fmtDateTime, fromNow } from '@/components/format'
import { notifyError, notifyOk } from '@/components/notify'
import { openModal } from '@/modals/open'
import { DataTableCard } from '@shared/ui/table'

const POLL_MS = 10_000
const POLL_BUSY_MS = 2_000

const JOB_KEYS = ['panel_users', 'traffic', 'panel_sync', 'backups'] as const
type JobKey = (typeof JOB_KEYS)[number]
type Status = 'running' | 'error' | 'disabled' | 'never' | 'ok'

const STATUS_COLOR: Record<Status, string> = { running: 'yellow', error: 'red', disabled: 'gray', never: 'gray', ok: 'teal' }

// statusOf: a failure outranks "disabled", so a broken job never looks quiet.
function statusOf(j: Job): Status {
    if (j.running) return 'running'
    if (j.last_ok === false) return 'error'
    if (!j.enabled || j.interval_seconds === null) return 'disabled'
    if (j.last_ok === null) return 'never'
    return 'ok'
}

const isJobKey = (k: string): k is JobKey => (JOB_KEYS as readonly string[]).includes(k)

type Unit = 'ms' | 's' | 'min' | 'h' | 'd'

// splitSpan picks the largest whole unit of a number of seconds.
function splitSpan(sec: number): [number, Unit] {
    if (sec % 86400 === 0) return [sec / 86400, 'd']
    if (sec % 3600 === 0) return [sec / 3600, 'h']
    if (sec % 60 === 0) return [sec / 60, 'min']
    return [sec, 's']
}

function useJobFormat() {
    const { t } = useTranslation()
    const name = (key: string) => (isJobKey(key) ? t(`jobs.name.${key}`) : key)
    const interval = (j: Job) => {
        if (!j.enabled || !j.interval_seconds) return t('jobs.disabled')
        const [n, unit] = splitSpan(j.interval_seconds)
        return t('jobs.every', { value: `${n} ${t(`jobs.unit.${unit}`)}` })
    }
    const duration = (ms: number) => {
        if (ms < 1000) return `${ms} ${t('jobs.unit.ms')}`
        if (ms < 60_000) return `${(ms / 1000).toFixed(1)} ${t('jobs.unit.s')}`
        const sec = Math.round(ms / 1000)
        return `${Math.floor(sec / 60)} ${t('jobs.unit.min')} ${sec % 60} ${t('jobs.unit.s')}`
    }
    return { name, interval, duration }
}

// When renders a relative time with the exact one in a tooltip.
function When({ at }: { at: string | null }) {
    if (!at) return <Text c="dimmed" size="sm">—</Text>
    return (
        <Tooltip label={fmtDateTime(at)} openDelay={200}>
            <Text maw={110} size="sm" style={{ whiteSpace: 'normal' }}>
                {fromNow(at)}
            </Text>
        </Tooltip>
    )
}

// ErrorBlock is a full error text in a monospace block with a copy button.
function ErrorBlock({ text }: { text: string }) {
    const { t } = useTranslation()
    return (
        <Box pos="relative">
            <Code block c="red" style={{ maxHeight: 220, overflow: 'auto', paddingRight: 40, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                {text}
            </Code>
            <CopyButton timeout={2000} value={text}>
                {({ copied, copy }) => (
                    <Tooltip label={copied ? t('jobs.copied') : t('jobs.copy')}>
                        <ActionIcon color={copied ? 'teal' : 'gray'} onClick={copy} pos="absolute" right={6} size="sm" top={6} variant="subtle">
                            {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                        </ActionIcon>
                    </Tooltip>
                )}
            </CopyButton>
        </Box>
    )
}

function FailureItem({ run }: { run: JobRun }) {
    const { t } = useTranslation()
    const repeated = run.repeat > 1
    return (
        <Stack gap={4}>
            <Group gap="xs">
                <Text className="num" fw={500} size="sm">
                    {fmtDateTime(repeated ? run.finished_at : run.started_at)}
                </Text>
                {repeated && (
                    <Badge color="red" variant="light">
                        ×{run.repeat}
                    </Badge>
                )}
            </Group>
            {repeated && (
                <Text c="dimmed" size="xs">
                    {t('jobs.repeated', { count: run.repeat })}: {t('jobs.first_last', { first: fmtDateTime(run.started_at), last: fmtDateTime(run.finished_at) })}
                </Text>
            )}
            <ErrorBlock text={run.text} />
        </Stack>
    )
}

// JobHistory is mounted only while its row is expanded, so history is
// fetched for the open row alone.
function JobHistory({ jobKey }: { jobKey: string }) {
    const { t } = useTranslation()
    const { duration } = useJobFormat()
    const q = useJobRuns(jobKey, true)
    if (!q.data) return <Loader size="sm" />
    const failures = q.data.filter((r) => !r.ok)
    const successes = q.data.filter((r) => r.ok)
    if (!q.data.length) {
        return (
            <Text c="dimmed" size="sm">
                {t('jobs.no_history')}
            </Text>
        )
    }
    return (
        <Stack gap="md">
            {failures.length > 0 && (
                <Stack gap="sm">
                    <Text c="dimmed" fw={600} size="xs" tt="uppercase">
                        {t('jobs.failures')} · {failures.length}
                    </Text>
                    {failures.map((r) => (
                        <FailureItem key={r.id} run={r} />
                    ))}
                </Stack>
            )}
            {successes.length > 0 && (
                <Stack gap={4}>
                    <Text c="dimmed" fw={600} size="xs" tt="uppercase">
                        {t('jobs.successes')} · {successes.length}
                    </Text>
                    {successes.map((r) => (
                        <Group gap="sm" key={r.id} wrap="nowrap">
                            <Text className="num" c="dimmed" size="sm" w={150}>
                                {fmtDateTime(r.started_at)}
                            </Text>
                            <Text flex={1} miw={0} size="sm">
                                {r.text || t('jobs.no_summary')}
                            </Text>
                            <Text c="dimmed" size="xs">
                                {duration(r.duration_ms)}
                            </Text>
                        </Group>
                    ))}
                </Stack>
            )}
        </Stack>
    )
}

// useRunJob runs a job on demand and reports the outcome in a notification.
function useRunJob() {
    const { t } = useTranslation()
    const { name } = useJobFormat()
    const run = useApiMutation((key: string) => api.post<Job>(`jobs/${key}/run`))
    return {
        isPending: run.isPending,
        pendingKey: run.isPending ? run.variables : undefined,
        run: (key: string) =>
            run.mutate(key, {
                onSuccess: (res) =>
                    res.last_ok === false
                        ? notifyError(new Error(res.last_error), t('jobs.run_failed', { name: name(key) }))
                        : notifyOk(res.last_summary || t('jobs.no_summary'), t('jobs.run_ok', { name: name(key) })),
                onError: (e) => notifyError(e)
            })
    }
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <Stack gap={2} miw={0}>
            <Text c="dimmed" size="xs">
                {label}
            </Text>
            <Box>{children}</Box>
        </Stack>
    )
}

// JobDetails is the content of the job popup: state, last error and run
// history. It follows the shared jobs query, so a running job updates live.
function JobDetails({ jobKey }: { jobKey: string }) {
    const { t } = useTranslation()
    const { duration, interval } = useJobFormat()
    const { isPending, run } = useRunJob()
    const jobs = useJobs(POLL_BUSY_MS)
    const j = jobs.data?.find((x) => x.key === jobKey)
    if (!j) return <Loader size="sm" />
    const s = statusOf(j)
    return (
        <Stack gap="lg">
            <Group justify="space-between" wrap="nowrap">
                <Badge color={STATUS_COLOR[s]} size="lg" variant="light">
                    {t(`jobs.status.${s}`)}
                </Badge>
                <Button
                    disabled={j.running || isPending}
                    leftSection={<IconPlayerPlay size={16} />}
                    loading={j.running || isPending}
                    onClick={() => run(j.key)}
                    variant="default"
                >
                    {t('jobs.run_now')}
                </Button>
            </Group>
            <SimpleGrid cols={{ base: 2, sm: 3 }} spacing="md">
                <Fact label={t('jobs.col_interval')}>
                    <Text size="sm">{interval(j)}</Text>
                </Fact>
                <Fact label={t('jobs.col_last_run')}>
                    <When at={j.last_run_at} />
                </Fact>
                <Fact label={t('jobs.col_last_success')}>
                    <When at={j.last_success_at} />
                </Fact>
                <Fact label={t('jobs.duration')}>
                    <Text className="num" size="sm">
                        {j.last_ok === null ? '—' : duration(j.last_duration_ms)}
                    </Text>
                </Fact>
                <Fact label={t('jobs.total_runs')}>
                    <Text className="num" size="sm">
                        {j.runs}
                    </Text>
                </Fact>
                <Fact label={t('jobs.total_failures')}>
                    <Text c={j.failures > 0 ? 'red' : undefined} className="num" size="sm">
                        {j.failures}
                    </Text>
                </Fact>
            </SimpleGrid>
            {j.last_summary && j.last_ok !== false && (
                <Fact label={t('jobs.last_result')}>
                    <Text size="sm">{j.last_summary}</Text>
                </Fact>
            )}
            {j.last_ok === false && j.last_error && (
                <Fact label={t('jobs.last_error')}>
                    <ErrorBlock text={j.last_error} />
                </Fact>
            )}
            <JobHistory jobKey={j.key} />
        </Stack>
    )
}

export function JobsPanel() {
    const { t } = useTranslation()
    const { name, interval, duration } = useJobFormat()
    const { isPending, pendingKey, run } = useRunJob()
    // the list refreshes slowly while idle and quickly while something runs
    const [busy, setBusy] = useState(false)
    const jobs = useJobs(busy || isPending ? POLL_BUSY_MS : POLL_MS)
    const anyRunning = jobs.data?.some((j) => j.running) ?? false
    useEffect(() => setBusy(anyRunning), [anyRunning])

    const columns = useMemo<MRT_ColumnDef<Job>[]>(
        () => [
            {
                id: 'name',
                header: t('jobs.col_name'),
                size: 190,
                minSize: 150,
                accessorFn: (j) => name(j.key),
                Cell: ({ row }) => (
                    <Text fw={500} maw={190} size="sm" style={{ whiteSpace: 'normal' }}>
                        {name(row.original.key)}
                    </Text>
                )
            },
            { id: 'interval', header: t('jobs.col_interval'), size: 100, minSize: 80, accessorFn: (j) => interval(j), Cell: ({ row }) => <Text size="sm">{interval(row.original)}</Text> },
            { id: 'last_run', header: t('jobs.col_last_run'), size: 115, minSize: 90, accessorFn: (j) => j.last_run_at, Cell: ({ row }) => <When at={row.original.last_run_at} /> },
            {
                id: 'status',
                header: t('jobs.col_status'),
                size: 130,
                minSize: 110,
                accessorFn: (j) => statusOf(j),
                Cell: ({ row }) => {
                    const j = row.original
                    const s = statusOf(j)
                    return (
                        <Stack align="flex-start" gap={2}>
                            <Badge color={STATUS_COLOR[s]} variant="light">
                                {t(`jobs.status.${s}`)}
                            </Badge>
                            {j.last_ok !== null && (
                                <Text c="dimmed" className="num" size="xs">
                                    {duration(j.last_duration_ms)}
                                </Text>
                            )}
                        </Stack>
                    )
                }
            }
        ],
        [t]
    )

    return (
        <DataTableCard
            columns={columns}
            data={jobs.data ?? []}
            description={t('jobs.hint')}
            displayColumnDefOptions={{
                'mrt-row-actions': { header: '', size: 56, minSize: 56, enableColumnOrdering: false }
            }}
            enableBottomToolbar={false}
            enableColumnActions={false}
            enableColumnFilters={false}
            enableColumnResizing={false}
            enableGlobalFilter={false}
            enablePagination={false}
            enableRowActions
            enableSorting={false}
            enableTopToolbar={false}
            getRowId={(j) => j.key}
            icon={<IconActivityHeartbeat />}
            onRowClick={(j) => openModal({ icon: IconActivityHeartbeat, subtitle: interval(j), title: name(j.key) }, () => <JobDetails jobKey={j.key} />, 'lg')}
            renderRowActions={({ row }) => {
                const j = row.original
                const pending = pendingKey === j.key
                return (
                    <Tooltip label={t('jobs.run_now')}>
                        <ActionIcon aria-label={t('jobs.run_now')} color="gray" disabled={j.running || pending} loading={j.running || pending} onClick={() => run(j.key)} variant="default">
                            <IconPlayerPlay size={16} />
                        </ActionIcon>
                    </Tooltip>
                )
            }}
            // no pinned columns: a narrow window scrolls cleanly instead of the actions column covering data
            state={{ isLoading: !jobs.data, columnPinning: { left: [], right: [] } }}
            storageKey="jobs"
            title={t('settings.jobs')}
        />
    )
}
