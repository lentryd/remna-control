import { Alert, Badge, Button, FileInput, Group, MultiSelect, NumberInput, Select, Stack, Switch, Text, Textarea, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import {
    IconAdjustmentsHorizontal,
    IconAlertTriangle,
    IconBuilding,
    IconCalendar,
    IconChartLine,
    IconCirclesRelation,
    IconClock,
    IconDevices,
    IconFileImport,
    IconTag,
    IconUser,
    IconUserPlus,
    IconUsersPlus
} from '@tabler/icons-react'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'

import { api } from '@/api/client'
import { useApiMutation, useInvalidateAll, useSquads, useTariffs } from '@/api/hooks'
import type { Company, CompanyMember, MemberOverrideField } from '@/api/types'
import i18n from '@/app/i18n/i18n'
import { fmtMoney, GB, STRATEGIES, strategyLabel } from '@/components/format'
import { errorText, notifyError, notifyOk } from '@/components/notify'
import { FormFooter, FormSection } from '@shared/ui/forms/form-section'
import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'

import { openModal } from './open'

// useBaseTariffs are the tariffs a company or an employee can be put on.
function useBaseTariffs() {
    const tariffs = useTariffs()
    const options = (tariffs.data ?? []).filter((t) => t.kind === 'base' && t.active).map((t) => ({ value: String(t.id), label: `${t.name} · ${fmtMoney(t.monthly_price)}` }))
    return { tariffs, options }
}

// openCompanyForm creates a company and opens its card.
export function openCompanyForm() {
    openModal({ icon: IconBuilding, color: 'brand', title: i18n.t('companies.new') }, (close) => <CompanyForm onDone={close} />, '560px')
}

function CompanyForm({ onDone }: { onDone: () => void }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const { options } = useBaseTariffs()
    const form = useForm({
        initialValues: { name: '', notes: '', tariff_id: '', report_day: 1 as number | string },
        validate: {
            name: (v) => (v.trim() ? null : t('tariffs.name_required')),
            tariff_id: (v) => (v ? null : t('companies.tariff_required')),
            report_day: (v) => (Number(v) >= 1 && Number(v) <= 28 ? null : t('companies.report_day_hint'))
        }
    })
    const m = useApiMutation((v: typeof form.values) =>
        api.post<{ id: number }>('companies', { name: v.name.trim(), notes: v.notes, tariff_id: Number(v.tariff_id), report_day: Number(v.report_day) })
    )
    return (
        <form
            onSubmit={form.onSubmit((v) =>
                m.mutate(v, {
                    onSuccess: (res) => {
                        notifyOk(t('companies.created'))
                        onDone()
                        navigate(`/companies/${res.id}`)
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap="md">
                <TextInput data-autofocus label={t('companies.name')} leftSection={<IconBuilding size={16} />} required {...form.getInputProps('name')} />
                <Select
                    data={options}
                    description={t('companies.tariff_hint')}
                    label={t('companies.tariff')}
                    leftSection={<IconTag size={16} />}
                    required
                    searchable
                    {...form.getInputProps('tariff_id')}
                />
                <NumberInput
                    allowDecimal={false}
                    description={t('companies.report_day_hint')}
                    label={t('companies.report_day')}
                    leftSection={<IconCalendar size={16} />}
                    max={28}
                    min={1}
                    {...form.getInputProps('report_day')}
                />
                <Textarea autosize label={t('expense_items.notes')} minRows={2} {...form.getInputProps('notes')} />
            </Stack>
            <FormFooter loading={m.isPending} onCancel={onDone} submitLabel={t('common.create')} />
        </form>
    )
}

// openAddMember adds one employee; a removed one with the same username comes back.
export function openAddMember(company: Company) {
    openModal({ icon: IconUserPlus, color: 'brand', title: i18n.t('companies.add_member'), subtitle: company.name }, (close) => <AddMemberForm company={company} onDone={close} />)
}

function AddMemberForm({ company, onDone }: { company: Company; onDone: () => void }) {
    const { t } = useTranslation()
    const form = useForm({
        initialValues: { username: '', label: '' },
        validate: { username: (v) => (v.trim() ? null : t('companies.username_required')) }
    })
    const m = useApiMutation((v: typeof form.values) => api.post(`companies/${company.id}/members`, { username: v.username.trim(), label: v.label.trim() }))
    return (
        <form
            onSubmit={form.onSubmit((v) =>
                m.mutate(v, {
                    onSuccess: () => {
                        notifyOk(t('companies.member_added'))
                        onDone()
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap="md">
                <Text c="dimmed" size="sm">
                    {t('companies.add_member_hint', { tariff: company.tariff_name })}
                </Text>
                <TextInput data-autofocus label={t('companies.username')} leftSection={<IconUser size={16} />} required {...form.getInputProps('username')} />
                <TextInput label={t('companies.label')} description={t('companies.label_hint')} {...form.getInputProps('label')} />
            </Stack>
            <FormFooter loading={m.isPending} onCancel={onDone} submitLabel={t('common.add')} />
        </form>
    )
}

// parseCsv reads "username,label" lines; a leading "username,label" header,
// blank lines and # comments are skipped.
export function parseCsv(text: string): { username: string; label: string }[] {
    return text
        .split(/\r?\n/)
        .map((l) => l.trim())
        .filter((l) => l && !l.startsWith('#'))
        .map((l) => {
            const i = l.search(/[,;\t]/)
            const username = (i < 0 ? l : l.slice(0, i)).trim().replace(/^"|"$/g, '')
            const label = (i < 0 ? '' : l.slice(i + 1)).trim().replace(/^"|"$/g, '')
            return { username, label }
        })
        .filter((r, i) => r.username && !(i === 0 && r.username.toLowerCase() === 'username'))
}

export function openImportMembers(company: Company) {
    openModal({ icon: IconFileImport, color: 'brand', title: i18n.t('companies.import'), subtitle: company.name }, (close) => <ImportForm company={company} onDone={close} />, '560px')
}

interface ImportFailure {
    line: number
    username: string
    error: string
}

function ImportForm({ company, onDone }: { company: Company; onDone: () => void }) {
    const { t } = useTranslation()
    const invalidate = useInvalidateAll()
    const [text, setText] = useState('')
    const [running, setRunning] = useState(false)
    const [progress, setProgress] = useState(0)
    const [result, setResult] = useState<{ added: number; failures: ImportFailure[] } | null>(null)
    const rows = parseCsv(text)

    const readFile = async (file: File | null) => {
        if (file) setText(await file.text())
    }
    const run = async () => {
        setRunning(true)
        const failures: ImportFailure[] = []
        let added = 0
        for (let i = 0; i < rows.length; i++) {
            setProgress(i)
            try {
                await api.post(`companies/${company.id}/members`, rows[i])
                added++
            } catch (e) {
                failures.push({ line: i + 1, username: rows[i].username, error: errorText(e) })
            }
        }
        setRunning(false)
        setResult({ added, failures })
        await invalidate()
    }

    if (result) {
        return (
            <Stack gap="md">
                <Alert color={result.failures.length ? 'yellow' : 'teal'} variant="soft">
                    {t('companies.import_done', { added: result.added, failed: result.failures.length })}
                </Alert>
                {result.failures.map((f) => (
                    <Group gap="xs" key={f.line} wrap="nowrap" align="flex-start">
                        <Badge color="red" variant="soft">
                            {f.line}
                        </Badge>
                        <Text size="sm">
                            <b>{f.username}</b>: {f.error}
                        </Text>
                    </Group>
                ))}
                <Group justify="flex-end">
                    <Button onClick={onDone} variant="default">
                        {t('common.close')}
                    </Button>
                </Group>
            </Stack>
        )
    }
    return (
        <Stack gap="md">
            <Text c="dimmed" size="sm">
                {t('companies.import_hint')}
            </Text>
            <FileInput accept=".csv,.txt,text/csv,text/plain" clearable label={t('companies.import_file')} onChange={readFile} placeholder="employees.csv" />
            <Textarea autosize label={t('companies.import_text')} minRows={6} maxRows={14} onChange={(e) => setText(e.currentTarget.value)} placeholder={'ivanov,Ivan Ivanov\npetrov,Petr Petrov'} value={text} />
            {running && <Text size="sm">{t('companies.import_progress', { done: progress + 1, total: rows.length })}</Text>}
            <FormFooter disabled={!rows.length} loading={running} onCancel={onDone} onSubmit={run} submitIcon={<IconUsersPlus size={16} />} submitLabel={t('companies.import_run', { count: rows.length })} />
        </Stack>
    )
}

// openMemberOverrides changes an employee's tariff and what differs from it.
export function openMemberOverrides(company: Company, member: CompanyMember) {
    openModal({ icon: IconTag, color: 'brand', title: i18n.t('companies.member_tariff'), subtitle: member.username }, (close) => <OverridesForm company={company} member={member} onDone={close} />, '620px')
}

function OverridesForm({ company, member, onDone }: { company: Company; member: CompanyMember; onDone: () => void }) {
    const { t } = useTranslation()
    const { tariffs, options } = useBaseTariffs()
    const squads = useSquads()
    const has = (f: MemberOverrideField) => member.overrides.includes(f)
    const form = useForm({
        initialValues: {
            tariff_id: String(member.tariff_id),
            o_price: has('price'),
            price: member.price,
            o_traffic: has('traffic_limit'),
            traffic_gb: member.traffic_limit_bytes ? Math.round((member.traffic_limit_bytes / GB) * 100) / 100 : 0,
            o_strategy: has('traffic_strategy'),
            traffic_strategy: member.traffic_strategy || 'NO_RESET',
            o_hwid: has('hwid_limit'),
            hwid_limit: (member.hwid_limit ?? '') as number | string,
            o_squads: has('squads'),
            squad_uuids: member.squad_uuids
        }
    })
    const tariff = tariffs.data?.find((x) => String(x.id) === form.values.tariff_id)
    const m = useApiMutation((v: typeof form.values) => {
        const reset: MemberOverrideField[] = []
        const body: Record<string, unknown> = { tariff_id: Number(v.tariff_id) }
        const field = (on: boolean, name: MemberOverrideField, set: () => void) => (on ? set() : reset.push(name))
        field(v.o_price, 'price', () => (body.price = Number(v.price)))
        field(v.o_traffic, 'traffic_limit', () => (body.traffic_limit_bytes = Math.round(Number(v.traffic_gb) * GB)))
        field(v.o_strategy, 'traffic_strategy', () => (body.traffic_strategy = v.traffic_strategy))
        field(v.o_hwid, 'hwid_limit', () => (body.hwid_limit = Number(v.hwid_limit || 0)))
        field(v.o_squads, 'squads', () => (body.squad_uuids = v.squad_uuids))
        body.reset = reset
        return api.put(`companies/${company.id}/members/${member.id}/override`, body)
    })

    // one overridable field: a switch, and the input while it is on; off shows the tariff's value
    const row = (on: 'o_price' | 'o_traffic' | 'o_strategy' | 'o_hwid' | 'o_squads', label: string, tariffValue: string, input: React.ReactNode) => (
        <Stack gap={6}>
            <Group justify="space-between" wrap="nowrap">
                <Switch checked={form.values[on]} label={label} onChange={(e) => form.setFieldValue(on, e.currentTarget.checked)} />
                {!form.values[on] && (
                    <Text c="dimmed" size="xs">
                        {t('companies.follows_tariff', { value: tariffValue })}
                    </Text>
                )}
            </Group>
            {form.values[on] && input}
        </Stack>
    )
    return (
        <form onSubmit={form.onSubmit((v) => m.mutate(v, { onSuccess: () => (notifyOk(t('companies.member_saved')), onDone()), onError: (e) => notifyError(e) }))}>
            <Stack gap="md">
                <Alert color="gray" variant="soft">
                    {t('companies.override_hint')}
                </Alert>
                <Select
                    allowDeselect={false}
                    data={options}
                    description={String(member.tariff_id) !== String(company.tariff_id) || member.tariff_pinned ? t('companies.pinned_hint') : t('companies.follows_company_hint')}
                    label={t('companies.tariff')}
                    leftSection={<IconTag size={16} />}
                    searchable
                    {...form.getInputProps('tariff_id')}
                />
                <FormSection icon={IconAdjustmentsHorizontal} title={t('companies.overrides')}>
                    {row(
                        'o_price',
                        t('companies.price_month'),
                        tariff ? fmtMoney(tariff.monthly_price) : '—',
                        <NumberInput decimalScale={2} min={0} {...form.getInputProps('price')} />
                    )}
                    {row(
                        'o_traffic',
                        t('tariffs.traffic_limit'),
                        tariff ? (tariff.traffic_limit_bytes ? `${Math.round((tariff.traffic_limit_bytes / GB) * 100) / 100} GB` : '∞') : '—',
                        <NumberInput decimalScale={2} description={t('tariffs.traffic_limit_hint')} leftSection={<IconChartLine size={16} />} min={0} {...form.getInputProps('traffic_gb')} />
                    )}
                    {row(
                        'o_strategy',
                        t('tariffs.traffic_reset'),
                        tariff ? strategyLabel(tariff.traffic_strategy) : '—',
                        <Select allowDeselect={false} data={STRATEGIES.map((value) => ({ value, label: strategyLabel(value) }))} leftSection={<IconClock size={16} />} {...form.getInputProps('traffic_strategy')} />
                    )}
                    {row(
                        'o_hwid',
                        t('tariffs.hwid'),
                        tariff ? (tariff.hwid_limit ?? '∞').toString() : '—',
                        <NumberInput description={t('tariffs.hwid_hint')} leftSection={<IconDevices size={16} />} min={0} {...form.getInputProps('hwid_limit')} />
                    )}
                    {row(
                        'o_squads',
                        t('tariffs.squads_label'),
                        tariff ? String(tariff.squad_uuids.length) : '—',
                        <MultiSelect
                            data={(squads.data ?? []).map((s) => ({ value: s.uuid, label: s.name }))}
                            leftSection={<IconCirclesRelation size={16} />}
                            placeholder={squads.isPending ? t('common.loading') : t('tariffs.choose')}
                            searchable
                            {...form.getInputProps('squad_uuids')}
                        />
                    )}
                </FormSection>
                <Text c="dimmed" size="xs">
                    {t('companies.override_segment_hint')}
                </Text>
            </Stack>
            <FormFooter loading={m.isPending} onCancel={onDone} />
        </form>
    )
}

// confirmCompany is a confirmation with a warning icon and a custom button colour.
export function confirmAction(title: string, text: React.ReactNode, onConfirm: () => void, confirmLabel: string, color = 'brand') {
    modals.openConfirmModal({
        title: <BaseOverlayHeader iconColor={color} IconComponent={IconAlertTriangle} title={title} />,
        children: text,
        labels: { confirm: confirmLabel, cancel: i18n.t('common.cancel') },
        confirmProps: { color, variant: 'filled' },
        cancelProps: { variant: 'default' },
        onConfirm
    })
}

// openTariffChange picks a new company tariff after a warning that every
// employee who follows the company tariff moves with it.
export function openTariffChange(company: Company) {
    openModal({ icon: IconTag, color: 'orange', title: i18n.t('companies.change_tariff'), subtitle: company.name }, (close) => <TariffChangeForm company={company} onDone={close} />)
}

function TariffChangeForm({ company, onDone }: { company: Company; onDone: () => void }) {
    const { t } = useTranslation()
    const { options } = useBaseTariffs()
    const [value, setValue] = useState<string | null>(String(company.tariff_id))
    const m = useApiMutation((id: number) => api.put(`companies/${company.id}/tariff`, { tariff_id: id }))
    const unchanged = !value || Number(value) === company.tariff_id
    return (
        <Stack gap="md">
            <Alert color="orange" icon={<IconAlertTriangle size={18} />} variant="soft">
                {t('companies.change_tariff_warning', { count: company.members_count })}
            </Alert>
            <Select allowDeselect={false} data={options} label={t('companies.tariff')} leftSection={<IconTag size={16} />} onChange={setValue} searchable value={value} />
            <FormFooter
                disabled={unchanged}
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={() =>
                    m.mutate(Number(value), {
                        onSuccess: () => (notifyOk(t('companies.tariff_changed')), onDone()),
                        onError: (e) => notifyError(e)
                    })
                }
                submitLabel={t('companies.change_tariff_apply')}
            />
        </Stack>
    )
}
