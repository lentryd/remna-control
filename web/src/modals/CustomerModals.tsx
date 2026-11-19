import { NumberInput, Stack, Switch, Text, Textarea, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { IconArchive, IconBrandTelegram, IconEdit, IconPercentage, IconScale, IconUser, IconUserPlus, IconUsersGroup } from '@tabler/icons-react'
import { useState } from 'react'
import { useNavigate } from 'react-router'

import { api } from '@/api/client'
import { useApiMutation, useCustomers } from '@/api/hooks'
import type { Customer } from '@/api/types'
import { notifyError, notifyOk } from '@/components/notify'
import { FieldGroup, FormFooter } from '@shared/ui/forms/form-section'

import { openModal } from './open'
import { openProvisionModal } from './SubscriptionModals'
import { SearchSelect } from '@shared/ui/forms/search-select'
import { CurrencyIcon } from '@shared/currencies'
import { currencySymbol } from '@/components/format'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

// openCustomerForm edits a customer or creates one; a new customer can go
// straight on to a new subscription (provision: false when the caller has
// its own next step, e.g. linking an existing panel user).
export function openCustomerForm(customer?: Customer, onCreated?: (id: number) => void, opts: { provision?: boolean } = {}) {
    openModal(
        customer
            ? { icon: IconUser, color: 'brand', title: i18n.t('sub.customer'), subtitle: customer.name }
            : { icon: IconUserPlus, color: 'brand', title: i18n.t('customers.new') },
        (close) => <CustomerForm customer={customer} offerProvision={!customer && opts.provision !== false} onCreated={onCreated} onDone={close} />,
        '560px'
    )
}

function CustomerForm({
    customer,
    offerProvision,
    onDone,
    onCreated
}: {
    customer?: Customer
    offerProvision: boolean
    onDone: () => void
    onCreated?: (id: number) => void
}) {
    const { t } = useTranslation()
    const [provision, setProvision] = useState(true)
    const customers = useCustomers()
    const navigate = useNavigate()
    const form = useForm({
        initialValues: {
            name: customer?.name ?? '',
            contact: customer?.contact ?? '',
            telegram_id: customer?.telegram_id ?? ('' as number | ''),
            notes: customer?.notes ?? '',
            referrer_id: customer?.referrer_id ? String(customer.referrer_id) : null,
            referral_percent: customer?.referral_percent ?? ('' as number | ''),
            archived: customer?.archived ?? false
        },
        validate: { name: (v) => (v.trim() ? null : t('customers.name_required')) }
    })
    const m = useApiMutation((v: typeof form.values) => {
        const body = {
            ...v,
            name: v.name.trim(),
            telegram_id: v.telegram_id === '' ? null : Number(v.telegram_id),
            referrer_id: v.referrer_id ? Number(v.referrer_id) : null,
            referral_percent: v.referral_percent === '' ? null : Number(v.referral_percent)
        }
        return customer ? api.put(`customers/${customer.id}`, body) : api.post<{ id: number }>('customers', body)
    })

    return (
        <form
            onSubmit={form.onSubmit((v) =>
                m.mutate(v, {
                    onSuccess: (res) => {
                        notifyOk(customer ? t('customers.saved') : t('customers.created'))
                        onDone()
                        const id = (res as { id?: number } | undefined)?.id
                        if (!customer && id) {
                            if (onCreated) onCreated(id)
                            else navigate(`/customers/${id}`)
                            if (offerProvision && provision) openProvisionModal({ id, name: v.name.trim() })
                        }
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap={0}>
                <FieldGroup title={t('sub.customer')}>
                    <TextInput
                        data-autofocus
                        description={t('customers.name_hint')}
                        label={t('customers.name')}
                        leftSection={<IconUser size={16} />}
                        required
                        {...form.getInputProps('name')}
                    />
                    <TextInput
                        label={t('customers.contact')}
                        leftSection={<IconBrandTelegram size={16} />}
                        placeholder={t('customers.contact_placeholder')}
                        {...form.getInputProps('contact')}
                    />
                    <NumberInput
                        allowDecimal={false}
                        allowNegative={false}
                        description={t('customers.telegram_id_hint')}
                        hideControls
                        label={t('customers.telegram_id')}
                        leftSection={<IconBrandTelegram size={16} />}
                        {...form.getInputProps('telegram_id')}
                    />
                    {customer && (
                        <Switch
                            description={t('customers.archived_hint')}
                            label={t('customers.archived')}
                            thumbIcon={<IconArchive size={10} />}
                            {...form.getInputProps('archived', { type: 'checkbox' })}
                        />
                    )}
                </FieldGroup>
                <FieldGroup description={t('dashboard.referrals_hint')} title={t('dashboard.referrals')}>
                    <SearchSelect
                            allowDeselect
                            clearable
                            data={(customers.data ?? []).filter((c) => c.id !== customer?.id).map((c) => ({ value: String(c.id), label: c.name }))}
                            label={t('customers.referrer')}
                            leftSection={<IconUsersGroup size={16} />}
                            placeholder={t('customers.no_referrer')}
                            {...form.getInputProps('referrer_id')}
                        />
                        <NumberInput
                            decimalScale={2}
                            description={t('customers.ref_percent_hint')}
                            label={t('customers.ref_percent')}
                            leftSection={<IconPercentage size={16} />}
                            max={100}
                            min={0}
                            {...form.getInputProps('referral_percent')}
                        />
                </FieldGroup>
                <FieldGroup title={t('expense_items.notes')}>
                    <Textarea autosize minRows={3} placeholder={t('customers.notes_placeholder')} {...form.getInputProps('notes')} />
                </FieldGroup>
            </Stack>
            {offerProvision && (
                <Switch
                    checked={provision}
                    description={t('customers.then_subscription_hint')}
                    label={t('customers.then_subscription')}
                    mt="md"
                    onChange={(e) => setProvision(e.currentTarget.checked)}
                />
            )}
            <FormFooter loading={m.isPending} onCancel={onDone} submitLabel={customer ? t('common.save') : t('common.create')} />
        </form>
    )
}

export function openAdjustModal(customer: { id: number; name: string }) {
    openModal({ icon: IconScale, color: 'yellow', title: i18n.t('customer.adjust_balance'), subtitle: customer.name }, (close) => <AdjustForm id={customer.id} onDone={close} />)
}

function AdjustForm({ id, onDone }: { id: number; onDone: () => void }) {
    const { t } = useTranslation()
    const form = useForm({ initialValues: { amount: 0, note: '' } })
    const m = useApiMutation((v: typeof form.values) => api.post(`customers/${id}/adjust`, v))
    return (
        <form
            onSubmit={form.onSubmit((v) =>
                m.mutate(v, {
                    onSuccess: () => {
                        notifyOk(t('customers.adjusted'))
                        onDone()
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap="md">
                <Text c="dimmed" size="sm">
                    {t('customers.adjust_hint')}
                </Text>
                    <NumberInput label={t('common.amount_in', { currency: currencySymbol() })} leftSection={<CurrencyIcon size={16} />} decimalScale={2} {...form.getInputProps('amount')} />
                    <TextInput label={t('customers.reason')} leftSection={<IconEdit size={16} />} required {...form.getInputProps('note')} />
            </Stack>
            <FormFooter loading={m.isPending} onCancel={onDone} submitLabel={t('customers.apply')} />
        </form>
    )
}
