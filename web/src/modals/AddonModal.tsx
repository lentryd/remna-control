import { SimpleGrid, Stack, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { IconLetterCase, IconPuzzle } from '@tabler/icons-react'

import { api } from '@/api/client'
import { useApiMutation } from '@/api/hooks'
import type { Addon } from '@/api/types'
import { notifyError, notifyOk } from '@/components/notify'
import { FieldGroup, FormFooter } from '@shared/ui/forms/form-section'

import { openModal } from './open'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

const STUB_STATUSES = ['LIMITED', 'EXPIRED', 'DISABLED'] as const

export function openAddonModal(addon?: Addon) {
    openModal(
        { icon: IconPuzzle, color: 'grape', title: addon ? i18n.t('tariffs.addon') : i18n.t('addon_form.new'), subtitle: addon?.name },
        (close) => <AddonForm addon={addon} onDone={close} />,
        'lg'
    )
}

function AddonForm({ addon, onDone }: { addon?: Addon; onDone: () => void }) {
    const { t } = useTranslation()
    const form = useForm({
        initialValues: {
            name: addon?.name ?? '',
            prefix: addon?.prefix ?? '',
            suffix: addon?.suffix ?? '',
            remark: addon?.remark ?? '',
            remark_unlimited: addon?.remark_unlimited ?? '',
            stubs: Object.fromEntries(STUB_STATUSES.map((s) => [s, addon?.stubs?.[s] ?? ''])) as Record<string, string>
        },
        validate: {
            prefix: (v, vals) => (v.trim() || vals.suffix.trim() ? null : t('errors.addon.prefix_required'))
        }
    })
    const m = useApiMutation((v: typeof form.values) => (addon ? api.put(`addons/${addon.id}`, v) : api.post('addons', v)))
    const v = form.values
    return (
        <form
            onSubmit={form.onSubmit((vals) =>
                m.mutate(vals, {
                    onSuccess: () => {
                        notifyOk(t('addon_form.saved'))
                        onDone()
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap={0}>
                <FieldGroup title={t('addon_form.user')} description={t('addon_form.user_hint')}>
                    <TextInput label={t('tariffs.col_name')} leftSection={<IconLetterCase size={16} />} placeholder="premium" {...form.getInputProps('name')} />
                    <SimpleGrid cols={2}>
                        <TextInput label={t('addon_form.prefix')} placeholder="premium_" {...form.getInputProps('prefix')} />
                        <TextInput label={t('addon_form.suffix')} placeholder="_premium" {...form.getInputProps('suffix')} />
                    </SimpleGrid>
                    <Text c="dimmed" size="xs">
                        {t('addon_form.username')} <b>{`${v.prefix}<username>${v.suffix}`}</b>
                    </Text>
                </FieldGroup>
                <FieldGroup title={t('addon_form.remarks')} description={t('addon_form.remarks_hint')}>
                    <TextInput label={t('addon_form.remark')} placeholder={t('addon_form.remark_placeholder')} {...form.getInputProps('remark')} />
                    <TextInput label={t('addon_form.remark_unlimited')} placeholder={t('addon_form.remark_unlimited_placeholder')} {...form.getInputProps('remark_unlimited')} />
                </FieldGroup>
                <FieldGroup title={t('addon_form.stubs')} description={t('addon_form.stubs_hint')}>
                    {STUB_STATUSES.map((s) => (
                        <TextInput key={s} label={s} {...form.getInputProps(`stubs.${s}`)} />
                    ))}
                </FieldGroup>
                <FormFooter loading={m.isPending} onCancel={onDone} />
            </Stack>
        </form>
    )
}
