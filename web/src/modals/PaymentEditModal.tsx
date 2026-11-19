import { Alert, Autocomplete, NumberInput, SimpleGrid, Stack, Text, TextInput } from '@mantine/core'
import { DateInput } from '@mantine/dates'
import { useForm } from '@mantine/form'
import dayjs from 'dayjs'
import { IconBuildingBank, IconCalendar, IconCoins, IconCreditCard, IconEdit, IconInfoCircle } from '@tabler/icons-react'

import { api } from '@/api/client'
import { useApiMutation } from '@/api/hooks'
import type { Payment } from '@/api/types'
import { fmtMoney, currencySymbol, dateLayout } from '@/components/format'
import { notifyError, notifyOk } from '@/components/notify'
import { FormFooter } from '@shared/ui/forms/form-section'

import { openModal } from './open'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

// paymentMethods are suggestions in the UI language; any text can be entered.
export const paymentMethods = () => i18n.t('payment.methods', { returnObjects: true }) as string[]

export function openPaymentEdit(p: Payment) {
    openModal(
        { icon: IconCreditCard, color: 'teal', title: i18n.t('payment.title'), subtitle: `${p.customer_name} · ${fmtMoney(p.amount, 2)}` },
        (close) => <PaymentEditForm onDone={close} payment={p} />,
        'lg'
    )
}

function PaymentEditForm({ payment, onDone }: { payment: Payment; onDone: () => void }) {
    const { t } = useTranslation()
    const form = useForm({
        initialValues: {
            amount: payment.amount as number | string,
            date: dayjs(payment.date).format('YYYY-MM-DD'),
            method: payment.method || null,
            note: payment.note
        },
        validate: {
            amount: (v) => (Number(v) > 0 ? null : t('errors.amount_positive')),
            date: (v) => (v ? null : t('errors.date_required'))
        }
    })
    const m = useApiMutation((v: typeof form.values) =>
        api.put(`payments/${payment.id}`, { ...v, amount: Number(v.amount), method: v.method ?? '' })
    )
    const suggestions = paymentMethods()
    const methods = payment.method && !suggestions.includes(payment.method) ? [...suggestions, payment.method] : suggestions

    return (
        <form
            onSubmit={form.onSubmit((v) =>
                m.mutate(v, {
                    onSuccess: () => {
                        notifyOk(t('payment.saved'))
                        onDone()
                    },
                    onError: (e) => notifyError(e)
                })
            )}
        >
            <Stack gap="md">
                <Text c="dimmed" size="sm">
                    {t('payment.edit_hint')}
                </Text>
                    <SimpleGrid cols={{ base: 1, xs: 2 }}>
                        <NumberInput
                            data-autofocus
                            decimalScale={2}
                            label={t('common.amount_in', { currency: currencySymbol() })}
                            leftSection={<IconCoins size={16} />}
                            min={0}
                            {...form.getInputProps('amount')}
                        />
                        <DateInput label={t('customer.col_date')} leftSection={<IconCalendar size={16} />} valueFormat={dateLayout()} {...form.getInputProps('date')} />
                        <Autocomplete data={methods} label={t('payment.method')} leftSection={<IconBuildingBank size={16} />} {...form.getInputProps('method')} />
                        <TextInput label={t('payment.comment')} leftSection={<IconEdit size={16} />} {...form.getInputProps('note')} />
                    </SimpleGrid>
                    {!payment.historical && (
                        <Alert color="gray" icon={<IconInfoCircle size={18} />}>
                            {t('payment.edit_effects')}
                        </Alert>
                    )}
                <FormFooter loading={m.isPending} onCancel={onDone} />
            </Stack>
        </form>
    )
}
