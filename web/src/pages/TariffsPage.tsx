import {
    ActionIcon,
    Box,
    Card,
    ThemeIcon,
    Tooltip,
    Alert,
    Badge,
    Button,
    Switch,
    Drawer,
    Group,
    MultiSelect,
    NumberInput,
    Radio,
    Select,
    SimpleGrid,
    Stack,
    Text,
    Textarea,
    TextInput
} from '@mantine/core'
import { useForm } from '@mantine/form'
import {
    PiCalendarDuotone,
    PiArchive,
    PiArrowCounterClockwise,
    PiClockDuotone,
    PiPencilSimple,
    PiPlus,
    PiPuzzlePieceDuotone,
    PiSortAscending,
    PiStackDuotone,
    PiTagDuotone,
    PiTextAa,
    PiTrash
} from 'react-icons/pi'
import { TbChartLine, TbCirclesRelation, TbDevices } from 'react-icons/tb'
import { useState } from 'react'

import { api } from '@/api/client'
import { useAddons, useApiMutation, useInvalidateAll, useSquads, useTariffs } from '@/api/hooks'
import type { Addon, Tariff } from '@/api/types'
import { durationLabel, fmtBytes, fmtMoney, GB, STRATEGIES, strategyLabel } from '@/components/format'
import { notifyError, notifyOk } from '@/components/notify'
import { PageHeader } from '@/components/ui'
import { openAddonModal } from '@/modals/AddonModal'
import { confirmDanger } from '@/modals/open'
import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'
import { Page } from '@shared/ui/page'
import { FormFooter, FormSection } from '@shared/ui/forms/form-section'
import { StatStrip } from '@shared/ui/stat-strip'
import { EntityAdd, EntityCard, EntityFigure, EntityGrid, EntityParam } from '@shared/ui/entity-card'
import { FooterFigure } from '@shared/ui/entity-card/footer-figure'

import classes from './tariffs.module.css'
import { CurrencyIcon } from '@shared/currencies'
import { useTranslation } from 'react-i18next'

type Draft = Partial<Tariff> & { kind: 'base' | 'addon' }

export function TariffsPage() {
    const { t } = useTranslation()
    const tariffs = useTariffs()
    const addons = useAddons()
    const [draft, setDraft] = useState<Draft | null>(null)
    const all = tariffs.data ?? []
    const archivedAddonIds = new Set((addons.data ?? []).filter((a) => a.archived).map((a) => a.id))
    const inArchive = (x: Tariff) => !x.active || (x.addon_id != null && archivedAddonIds.has(x.addon_id))
    const base = all.filter((t) => t.kind === 'base' && !inArchive(t))
    const addonTariffs = all.filter((t) => t.kind === 'addon' && !inArchive(t))
    const archivedTariffs = all.filter(inArchive)
    const liveAddons = (addons.data ?? []).filter((a) => !a.archived)
    const archivedAddons = (addons.data ?? []).filter((a) => a.archived)
    const invalidate = useInvalidateAll()
    const setAddonArchived = async (a: Addon, archive: boolean) => {
        try {
            await api.post(`addons/${a.id}/${archive ? 'archive' : 'restore'}`)
            await invalidate()
        } catch (e) {
            notifyError(e)
        }
    }
    const archiveAddon = (a: Addon) =>
        confirmDanger(t('tariffs.archive_addon', { name: a.name }), t('tariffs.archive_hint'), () => setAddonArchived(a, true))
    const deleteAddon = (a: Addon) =>
        confirmDanger(t('tariffs.delete_addon', { name: a.name }), t('tariffs.delete_addon_hint'), async () => {
            try {
                await api.del(`addons/${a.id}`)
                await invalidate()
            } catch (e) {
                notifyError(e)
            }
        })

    return (
        <Page title={t('menu.tariffs_addons')}>
            <PageHeader
                title={t('menu.tariffs_addons')}
                description={t('tariffs.description')}
                actions={
                    <>
                        <Button leftSection={<PiPlus size={16} />} onClick={() => openAddonModal()} variant="default">
                            {t('tariffs.addon')}
                        </Button>
                        <Button leftSection={<PiPlus size={16} />} onClick={() => setDraft({ kind: 'base', active: true })} variant="filled">
                            {t('tariffs.tariff')}
                        </Button>
                    </>
                }
            />

            <StatStrip
                items={[
                    { label: t('tariffs.stat_tariffs'), value: all.length, hint: t('tariffs.stat_active', { count: all.filter((x) => x.active).length }) },
                    { label: t('tariffs.stat_subscribers'), value: all.reduce((n, x) => n + x.subscribers, 0) },
                    { label: t('tariffs.stat_mrr'), value: fmtMoney(all.reduce((n, x) => n + x.mrr, 0)) },
                    { label: t('tariffs.stat_addons'), value: addons.data?.length ?? 0 }
                ]}
                mb="xl"
            />

            <SectionTitle description={t('tariffs.base_hint')} title={t('tariffs.base_title')} />
            <EntityGrid>
                {base.map((tf) => (
                    <TariffCard key={tf.id} onEdit={() => setDraft(tf)} tariff={tf} />
                ))}
                {tariffs.data && base.length === 0 && <EntityAdd label={t('tariffs.new_tariff')} onClick={() => setDraft({ kind: 'base', active: true })} />}
            </EntityGrid>

            <SectionTitle description={t('tariffs.addons_hint')} mt={40} title={t('tariffs.addons_title')} />
            <Stack gap="lg">
                {liveAddons.length === 0 && <Alert color="gray">{t('tariffs.no_addons')}</Alert>}
                {liveAddons.map((a) => {
                    const rows = addonTariffs.filter((x) => x.addon_id === a.id)
                    return (
                        <Card className={classes.addon} key={a.id} padding={0}>
                            <div className={classes.addonHeader}>
                                <Group gap="sm" miw={0} wrap="nowrap">
                                    <ThemeIcon color="grape" radius="md" size={34}>
                                        <PiPuzzlePieceDuotone size={18} />
                                    </ThemeIcon>
                                    <Box miw={0}>
                                        <Group gap="xs">
                                            <Text c="var(--app-text-strong)" fw={600}>
                                                {a.name}
                                            </Text>
                                            {a.source === 'file' && (
                                                <Badge color="gray">{a.in_config ? t('tariffs.from_file') : t('tariffs.removed_from_file')}</Badge>
                                            )}
                                        </Group>
                                        <Text c="dimmed" size="xs">
                                            {t('tariffs.addon_user', { name: `${a.prefix}<username>${a.suffix}` })}
                                        </Text>
                                    </Box>
                                </Group>
                                <Group gap={4} wrap="nowrap">
                                    {a.source === 'ui' && (
                                        <>
                                            <Tooltip label={t('common.edit')}>
                                                <ActionIcon onClick={() => openAddonModal(a)} size="lg">
                                                    <PiPencilSimple size={16} />
                                                </ActionIcon>
                                            </Tooltip>
                                            <Tooltip label={t('tariffs.archive')}>
                                                <ActionIcon color="red" onClick={() => archiveAddon(a)} size="lg">
                                                    <PiArchive size={16} />
                                                </ActionIcon>
                                            </Tooltip>
                                        </>
                                    )}
                                    <Button
                                        leftSection={<PiPlus size={14} />}
                                        ml={4}
                                        onClick={() => setDraft({ kind: 'addon', addon_id: a.id, active: true, manage_rw: true })}
                                        size="xs"
                                        variant="default"
                                    >
                                        {t('tariffs.addon_tariff')}
                                    </Button>
                                </Group>
                            </div>
                            {rows.length ? (
                                <div className={classes.addonGrid}>
                                    <EntityGrid>
                                        {rows.map((tf) => (
                                            <TariffCard key={tf.id} onEdit={() => setDraft(tf)} tariff={tf} />
                                        ))}
                                    </EntityGrid>
                                </div>
                            ) : (
                                <Text c="dimmed" p="lg" size="sm">
                                    {t('tariffs.no_tariffs_yet')}
                                </Text>
                            )}
                        </Card>
                    )
                })}
            </Stack>

            {(archivedTariffs.length > 0 || archivedAddons.length > 0) && (
                <>
                    <SectionTitle mt={40} title={t('tariffs.archive_group')} />
                    <Stack gap="lg">
                        {archivedAddons.length > 0 && (
                            <Stack gap="xs">
                                {archivedAddons.map((a) => (
                                    <Card key={a.id} p="sm" withBorder>
                                        <Group justify="space-between" wrap="nowrap">
                                            <Group gap="sm" miw={0} wrap="nowrap">
                                                <ThemeIcon color="gray" radius="md" size={34}>
                                                    <PiPuzzlePieceDuotone size={18} />
                                                </ThemeIcon>
                                                <Text c="dimmed" fw={600}>
                                                    {a.name}
                                                </Text>
                                            </Group>
                                            <Group gap={4} wrap="nowrap">
                                                <Tooltip label={t('tariffs.restore')}>
                                                    <ActionIcon onClick={() => setAddonArchived(a, false)} size="lg">
                                                        <PiArrowCounterClockwise size={16} />
                                                    </ActionIcon>
                                                </Tooltip>
                                                <Tooltip label={t('common.delete')}>
                                                    <ActionIcon color="red" onClick={() => deleteAddon(a)} size="lg">
                                                        <PiTrash size={16} />
                                                    </ActionIcon>
                                                </Tooltip>
                                            </Group>
                                        </Group>
                                    </Card>
                                ))}
                            </Stack>
                        )}
                        <EntityGrid>
                            {archivedTariffs.map((tf) => (
                                <TariffCard key={tf.id} onEdit={() => setDraft(tf)} tariff={tf} />
                            ))}
                        </EntityGrid>
                    </Stack>
                </>
            )}

            <Drawer
                opened={!!draft}
                onClose={() => setDraft(null)}
                position="right"
                size="lg"
                title={
                    <BaseOverlayHeader
                        IconComponent={PiTagDuotone}
                        subtitle={draft?.id ? draft.name : undefined}
                        title={draft?.id ? t('tariffs.tariff') : t('tariffs.new_tariff')}
                    />
                }
            >
                {draft && <TariffForm draft={draft} addons={addons.data ?? []} onDone={() => setDraft(null)} />}
            </Drawer>
        </Page>
    )
}

function SectionTitle({ title, description, mt }: { title: string; description?: string; mt?: number }) {
    return (
        <Box mb="md" mt={mt}>
            <Text c="var(--app-text-strong)" fw={600} fz={17} style={{ letterSpacing: '-0.01em' }}>
                {title}
            </Text>
            {description && (
                <Text c="dimmed" size="sm">
                    {description}
                </Text>
            )}
        </Box>
    )
}

// TariffCard is one tariff: price and periods, what it sets in the panel,
// and how many pay for it. A click opens it for editing.
function TariffCard({ tariff: tf, onEdit }: { tariff: Tariff; onEdit: () => void }) {
    const { t } = useTranslation()
    const invalidate = useInvalidateAll()
    const setArchived = async (archive: boolean) => {
        try {
            await api.post(`tariffs/${tf.id}/${archive ? 'archive' : 'restore'}`)
            await invalidate()
        } catch (e) {
            notifyError(e)
        }
    }
    const archive = () => confirmDanger(t('tariffs.archive_tariff', { name: tf.name }), t('tariffs.archive_hint'), () => setArchived(true))
    const remove = () =>
        confirmDanger(t('tariffs.delete', { name: tf.name }), t('tariffs.delete_hint'), async () => {
            try {
                await api.del(`tariffs/${tf.id}`)
                await invalidate()
            } catch (e) {
                notifyError(e)
            }
        })
    return (
        <EntityCard
            badges={!tf.active && <Badge color="gray">{t('tariffs.hidden')}</Badge>}
            dimmed={!tf.active}
            dot={tf.active ? 'teal' : 'gray'}
            footer={
                <>
                    <FooterFigure label={t('tariffs.col_subscribers')}>
                        {tf.subscribers}
                        {tf.overridden > 0 && (
                            <Text c="yellow" component="span" fw={500} size="xs">
                                {` ${t('tariffs.overridden', { count: tf.overridden })}`}
                            </Text>
                        )}
                    </FooterFigure>
                    <FooterFigure align="right" label={t('tariffs.stat_mrr')}>
                        {fmtMoney(tf.mrr)}
                    </FooterFigure>
                </>
            }
            menu={[
                { label: t('common.edit'), icon: PiPencilSimple, onClick: onEdit },
                tf.active
                    ? { label: t('tariffs.archive'), icon: PiArchive, color: 'red', onClick: archive }
                    : { label: t('tariffs.restore'), icon: PiArrowCounterClockwise, onClick: () => setArchived(false) },
                ...(tf.active ? [] : [{ label: t('common.delete'), icon: PiTrash, color: 'red', onClick: remove }])
            ]}
            onClick={onEdit}
            params={
                tf.manage_rw ? (
                    <>
                        <EntityParam icon={TbChartLine} label={t('tariffs.traffic')}>
                            {tf.traffic_limit_bytes ? fmtBytes(tf.traffic_limit_bytes) : '∞'}
                            <Text c="dimmed" component="span" inherit>
                                {' · '}
                                {strategyLabel(tf.traffic_strategy)}
                            </Text>
                        </EntityParam>
                        {tf.hwid_limit !== null && (
                            <EntityParam icon={TbDevices} label="HWID">
                                {t('tariffs.devices', { count: tf.hwid_limit })}
                            </EntityParam>
                        )}
                        <EntityParam icon={TbCirclesRelation} label={t('tariffs.squads_label')}>
                            {t('tariffs.squads', { count: tf.squad_uuids.length })}
                        </EntityParam>
                    </>
                ) : (
                    <Text c="dimmed" size="xs">
                        {t('tariffs.col_panel')}: {t('tariffs.not_managed')}
                    </Text>
                )
            }
            subtitle={tf.description}
            title={tf.name}
        >
            <EntityFigure unit={t('tariffs.per_month')} value={fmtMoney(tf.monthly_price)} />

            {(tf.periods.length > 0 || tf.included_addon_tariff_ids?.length > 0) && (
                <Group gap={6} mt="sm">
                    {tf.periods.map((p) => (
                        <Badge color="gray" key={`${p.months}:${p.days}`}>
                            {durationLabel(p.months, p.days)} · {fmtMoney(p.price)}
                        </Badge>
                    ))}
                    {tf.included_addon_tariff_ids?.length > 0 && (
                        <Badge color="grape" leftSection={<PiPuzzlePieceDuotone size={12} />}>
                            {t('tariffs.plus_addons', { count: tf.included_addon_tariff_ids.length })}
                        </Badge>
                    )}
                </Group>
            )}
        </EntityCard>
    )
}

function TariffForm({ draft, addons, onDone }: { draft: Draft; addons: Addon[]; onDone: () => void }) {
    const { t } = useTranslation()
    const squads = useSquads()
    const tariffs = useTariffs()
    const addonTariffs = (tariffs.data ?? []).filter((t) => t.kind === 'addon')
    const form = useForm({
        initialValues: {
            kind: draft.kind,
            addon_id: draft.addon_id ? String(draft.addon_id) : null,
            name: draft.name ?? '',
            description: draft.description ?? '',
            monthly_price: draft.monthly_price ?? 0,
            active: draft.active ?? true,
            sort_order: draft.sort_order ?? 0,
            manage_rw: draft.manage_rw ?? false,
            traffic_gb: draft.traffic_limit_bytes ? Math.round((draft.traffic_limit_bytes / GB) * 100) / 100 : 0,
            traffic_strategy: draft.traffic_strategy ?? 'NO_RESET',
            hwid_limit: draft.hwid_limit ?? ('' as number | ''),
            squad_uuids: draft.squad_uuids ?? [],
            periods: (draft.periods ?? []).map((p) => ({ ...p, days: p.days ?? 0 })),
            included: (draft.included_addon_tariff_ids ?? []).map(String),
            // what to do with current subscribers when included add-ons change
            sync_included: 'skip' as 'skip' | 'disable' | 'keep_paid',
            price_change: 'keep'
        },
        validate: {
            name: (v) => (v.trim() ? null : t('tariffs.name_required')),
            addon_id: (v, vals) => (vals.kind === 'addon' && !v ? t('errors.tariff.addon_required') : null),
            included: (v) => {
                const ids = v.map((id) => addonTariffs.find((t) => String(t.id) === id)?.addon_id)
                return new Set(ids).size === ids.length ? null : t('tariffs.one_per_addon')
            }
        }
    })
    const priceChanged = !!draft.id && form.values.monthly_price !== draft.monthly_price && (draft.subscribers ?? 0) > 0

    const m = useApiMutation((v: typeof form.values) => {
        const body = {
            kind: v.kind,
            addon_id: v.addon_id ? Number(v.addon_id) : null,
            name: v.name.trim(),
            description: v.description,
            monthly_price: v.monthly_price,
            active: v.active,
            sort_order: v.sort_order,
            manage_rw: v.manage_rw,
            traffic_limit_bytes: Math.round(v.traffic_gb * GB),
            traffic_strategy: v.traffic_strategy,
            hwid_limit: v.hwid_limit === '' ? null : Number(v.hwid_limit),
            squad_uuids: v.squad_uuids,
            periods: v.periods.filter((p) => p.months + p.days > 0 && !(p.months === 1 && !p.days)),
            included_addon_tariff_ids: v.kind === 'base' ? v.included.map(Number) : [],
            price_change: v.price_change
        }
        if (!draft.id) return api.post('tariffs', body)
        return api.put(`tariffs/${draft.id}`, body).then(() =>
            includedChanged && v.sync_included !== 'skip'
                ? api.post(`tariffs/${draft.id}/sync-included`, { removed: v.sync_included })
                : undefined
        )
    })
    const includedChanged =
        !!draft.id &&
        (draft.subscribers ?? 0) > 0 &&
        [...form.values.included].sort().join() !== (draft.included_addon_tariff_ids ?? []).map(String).sort().join()

    return (
        <form
            onSubmit={form.onSubmit((v) =>
                m.mutate(v, {
                    onSuccess: () => {
                        notifyOk(t('tariffs.saved'))
                        onDone()
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap="md">
                <FormSection icon={PiTagDuotone} title={t('tariffs.tariff')} description={t('tariffs.section_main')}>
                    <SimpleGrid cols={{ base: 1, xs: 2 }}>
                        <Select
                            label={t('tariffs.kind')}
                            leftSection={<PiStackDuotone size={16} />}
                            disabled={!!draft.id}
                            data={[
                                { value: 'base', label: t('tariffs.kind_base') },
                                { value: 'addon', label: t('tariffs.addon') }
                            ]}
                            {...form.getInputProps('kind')}
                        />
                        {form.values.kind === 'addon' && (
                            <Select
                                label={t('tariffs.addon')}
                                leftSection={<PiPuzzlePieceDuotone size={16} />}
                                disabled={!!draft.id}
                                data={addons.map((a) => ({ value: String(a.id), label: a.name }))}
                                {...form.getInputProps('addon_id')}
                            />
                        )}
                    </SimpleGrid>
                    <TextInput label={t('tariffs.col_name')} leftSection={<PiTextAa size={16} />} required {...form.getInputProps('name')} />
                    <Textarea label={t('tariffs.description_label')} autosize minRows={2} {...form.getInputProps('description')} />
                    {form.values.kind === 'base' && (
                        <MultiSelect
                            label={t('tariffs.included')}
                            description={t('tariffs.included_hint')}
                            leftSection={<PiPuzzlePieceDuotone size={16} />}
                            placeholder={addonTariffs.length ? t('tariffs.no_included') : t('tariffs.no_addon_tariffs')}
                            data={addonTariffs.map((t) => ({ value: String(t.id), label: `${t.addon_name} · ${t.name}` }))}
                            clearable
                            {...form.getInputProps('included')}
                        />
                    )}
                    {includedChanged && (
                        <Select
                            label={t('tariffs.current_subscribers')}
                            description={t('tariffs.subscribers_count', { count: draft.subscribers })}
                            leftSection={<PiPuzzlePieceDuotone size={16} />}
                            data={[
                                { value: 'skip', label: t('tariffs.sync_skip') },
                                { value: 'disable', label: t('tariffs.sync_disable') },
                                { value: 'keep_paid', label: t('tariffs.sync_keep') }
                            ]}
                            allowDeselect={false}
                            {...form.getInputProps('sync_included')}
                        />
                    )}
                    <SimpleGrid cols={{ base: 1, xs: 2 }}>
                        <NumberInput
                            label={t('tariffs.monthly_price')}
                            leftSection={<CurrencyIcon size={16} />}
                            min={0}
                            decimalScale={2}
                            {...form.getInputProps('monthly_price')}
                        />
                        <NumberInput label={t('tariffs.sort_order')} leftSection={<PiSortAscending size={16} />} {...form.getInputProps('sort_order')} />
                    </SimpleGrid>
                    {priceChanged && (
                        <Radio.Group label={t('tariffs.price_changes', { count: draft.subscribers })} {...form.getInputProps('price_change')}>
                            <Stack gap={6} mt={6}>
                                <Radio value="keep" label={t('tariffs.price_keep')} />
                                <Radio value="apply" label={t('tariffs.price_apply')} />
                            </Stack>
                        </Radio.Group>
                    )}
                    <Switch
                        label={t('tariffs.active_label')}
                        description={t('tariffs.active_hint')}
                        {...form.getInputProps('active', { type: 'checkbox' })}
                    />
                </FormSection>

                <FormSection
                    icon={PiCalendarDuotone}
                    color="teal"
                    title={t('tariffs.col_periods')}
                    description={t('tariffs.periods_hint')}
                    actions={
                        <Button
                            leftSection={<PiPlus size={14} />}
                            onClick={() => form.insertListItem('periods', { months: 3, days: 0, price: form.values.monthly_price * 3 })}
                            size="xs"
                            variant="default"
                        >
                            {t('tariffs.period')}
                        </Button>
                    }
                >
                    {form.values.periods.length === 0 && (
                        <Text c="dimmed" size="sm">
                            {t('tariffs.no_periods')}
                        </Text>
                    )}
                    {form.values.periods.map((_, i) => (
                        <Group key={i} wrap="nowrap" align="flex-end">
                            <NumberInput
                                label={t('tariffs.months')}
                                leftSection={<PiCalendarDuotone size={16} />}
                                min={0}
                                max={36}
                                {...form.getInputProps(`periods.${i}.months`)}
                            />
                            <NumberInput
                                label={t('tariffs.days')}
                                leftSection={<PiClockDuotone size={16} />}
                                min={0}
                                max={365}
                                {...form.getInputProps(`periods.${i}.days`)}
                            />
                            <NumberInput
                                label={t('tariffs.period_price')}
                                leftSection={<CurrencyIcon size={16} />}
                                min={0}
                                decimalScale={2}
                                {...form.getInputProps(`periods.${i}.price`)}
                            />
                            <ActionIcon color="red" variant="soft" size="input-sm" onClick={() => form.removeListItem('periods', i)}>
                                <PiTrash size={16} />
                            </ActionIcon>
                        </Group>
                    ))}
                </FormSection>

                <FormSection icon={TbChartLine} color="violet" title={t('tariffs.panel_section')} description={t('tariffs.panel_section_hint')}>
                    <Switch
                        label={t('tariffs.manage_rw')}
                        description={t('tariffs.manage_rw_hint')}
                        {...form.getInputProps('manage_rw', { type: 'checkbox' })}
                    />
                    {form.values.manage_rw && (
                        <>
                            <SimpleGrid cols={{ base: 1, xs: 2 }}>
                                <NumberInput
                                    label={t('tariffs.traffic_limit')}
                                    description={t('tariffs.traffic_limit_hint')}
                                    leftSection={<TbChartLine size={16} />}
                                    min={0}
                                    decimalScale={2}
                                    {...form.getInputProps('traffic_gb')}
                                />
                                <Select
                                    label={t('tariffs.traffic_reset')}
                                    description={t('tariffs.traffic_reset_hint')}
                                    leftSection={<PiClockDuotone size={16} />}
                                    data={STRATEGIES.map((value) => ({ value, label: strategyLabel(value) }))}
                                    {...form.getInputProps('traffic_strategy')}
                                />
                            </SimpleGrid>
                            {form.values.traffic_strategy === 'SUBSCRIPTION' && (
                                <Text c="dimmed" size="xs">
                                    {t('tariffs.traffic_reset_subscription_hint')}
                                </Text>
                            )}
                            <NumberInput
                                label={t('tariffs.hwid')}
                                description={t('tariffs.hwid_hint')}
                                leftSection={<TbDevices size={16} />}
                                min={0}
                                {...form.getInputProps('hwid_limit')}
                            />
                            <MultiSelect
                                label={t('tariffs.squads_label')}
                                leftSection={<TbCirclesRelation size={16} />}
                                placeholder={squads.isPending ? t('common.loading') : t('tariffs.choose')}
                                data={(squads.data ?? []).map((s) => ({ value: s.uuid, label: s.name, members: s.info.membersCount }))}
                                renderOption={({ option }) => {
                                    const sq = squads.data?.find((x) => x.uuid === option.value)
                                    return (
                                        <Group flex="1" gap="xs" wrap="nowrap">
                                            <Text size="sm" truncate="end">
                                                {option.label}
                                            </Text>
                                            <Badge color="gray" ml="auto" size="sm" variant="light">
                                                {sq?.info.membersCount ?? 0}
                                            </Badge>
                                        </Group>
                                    )
                                }}
                                searchable
                                {...form.getInputProps('squad_uuids')}
                            />
                            {squads.error && <Alert color="red" variant="soft">{squads.error.message}</Alert>}
                        </>
                    )}
                </FormSection>
                <FormFooter inline loading={m.isPending} onCancel={onDone} />
            </Stack>
        </form>
    )
}
