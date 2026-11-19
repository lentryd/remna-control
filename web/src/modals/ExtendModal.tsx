import { Alert } from '@mantine/core'
import { IconCalendarPlus, IconCheck } from '@tabler/icons-react'
import { useDebouncedValue } from '@mantine/hooks'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'

import { api } from '@/api/client'
import { useApiMutation } from '@/api/hooks'
import type { ExtensionResult, Period, ReferralInfo } from '@/api/types'
import { fmtDate, fmtMoney } from '@/components/format'
import { notifyError, notifyOk } from '@/components/notify'
import {
    type Income,
    IncomeSection,
    incomeFor,
    newIncome,
    PaymentSection,
    type Source,
    SourceSwitch,
    tariffPresets,
    type Term,
    TermSection
} from '@/components/term'
import { FormColumns, FormFooter } from '@shared/ui/forms/form-section'

import { openModal } from './open'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

type Kind = 'subscription' | 'addon'

interface ExtendQuote {
    customer_id: number
    amount: number
    from: string
    to: string
    monthly: number
    periods: Period[]
    balance: number
}

interface PaymentResult {
    extensions: ExtensionResult[] | null
    balance: number
    referral: ReferralInfo | null
}

// openExtendModal renews a subscription or add-on: either the customer has
// just paid (the payment is recorded and spent on this item in one go) or
// the term is charged to their balance.
export function openExtendModal(p: { kind: Kind; id: number; title: string }) {
    openModal({ icon: IconCalendarPlus, color: 'teal', title: i18n.t('sub.renewal'), subtitle: p.title }, (close) => <ExtendForm {...p} onDone={close} />, '1000px')
}

function ExtendForm({ kind, id, onDone }: { kind: Kind; id: number; onDone: () => void }) {
    const { t } = useTranslation()
    const [term, setTerm] = useState<Term>({ months: 1, days: 0, until: null })
    const [amount, setAmount] = useState<number | null>(null)
    const [touched, setTouched] = useState(false)
    const [allowDebt, setAllowDebt] = useState(false)
    // null until the first quote picks a default
    const [source, setSource] = useState<Source | null>(null)
    const [income, setIncome] = useState<Income>(newIncome)
    const [incomeTouched, setIncomeTouched] = useState(false)
    const [debounced] = useDebouncedValue(term, 250)

    const quote = useQuery({
        queryKey: ['quote', kind, id, debounced.months, debounced.days],
        queryFn: () => api.get<ExtendQuote>(`items/${kind}/${id}/quote?months=${debounced.months}&days=${debounced.days}`),
        placeholderData: keepPreviousData
    })
    useEffect(() => {
        if (!touched && quote.data) setAmount(quote.data.amount)
    }, [quote.data, touched])
    // The balance covers it → charge it; otherwise the customer is paying.
    useEffect(() => {
        if (source === null && quote.data) setSource(quote.data.balance >= quote.data.amount ? 'balance' : 'paid')
    }, [quote.data, source])
    // The payment follows the charge until edited by hand.
    useEffect(() => {
        if (!incomeTouched && quote.data) setIncome((v) => ({ ...v, amount: incomeFor(amount ?? 0, quote.data!.balance) }))
    }, [amount, quote.data, incomeTouched])

    const paid = source === 'paid'
    // either way the result is the extension (absent if nothing was extended)
    const m = useApiMutation((): Promise<ExtensionResult | undefined> =>
        paid
            ? api.post<PaymentResult>(`customers/${quote.data!.customer_id}/payments`, {
                  amount: income.amount,
                  date: income.date,
                  method: income.method ?? '',
                  note: '',
                  allocations: [{ kind, id, months: term.months, days: term.days, amount: amount ?? 0 }]
              }).then((r) => r.extensions?.[0])
            : api.post<ExtensionResult>(`items/${kind}/${id}/extend`, { months: term.months, days: term.days, amount: amount ?? 0, allow_debt: allowDebt })
    )
    const submit = () =>
        m.mutate(undefined, {
            onSuccess: (ext) => {
                if (paid) {
                    // the payment is saved either way; a failed extension is refunded to the balance
                    if (ext && !ext.ok) {
                        notifyError(new Error(t('extend.paid_failed', { amount: fmtMoney(income.amount, 2), error: ext.error })))
                        onDone()
                        return
                    }
                    notifyOk(t('extend.paid_done', { amount: fmtMoney(income.amount, 2), date: fmtDate(ext?.to) }))
                } else {
                    notifyOk(t('extend.done', { date: fmtDate(ext?.to) }))
                }
                onDone()
            },
            onError: (e) => notifyError(e)
        })

    return (
        <>
            <FormColumns
                left={
                    <TermSection
                        from={quote.data?.from}
                        onChange={(t) => {
                            setTerm(t)
                            setTouched(false)
                        }}
                        presets={tariffPresets(quote.data?.monthly, quote.data?.periods)}
                        value={term}
                    >
                        {quote.error && (
                            <Alert color="red" variant="soft">
                                {quote.error.message}
                            </Alert>
                        )}
                    </TermSection>
                }
                right={
                    <>
                        <SourceSwitch onChange={setSource} value={source ?? 'paid'} />
                        {paid && (
                            <IncomeSection
                                onChange={(v) => {
                                    setIncomeTouched(true)
                                    setIncome(v)
                                }}
                                value={income}
                            />
                        )}
                        <PaymentSection
                            allowDebt={allowDebt}
                            amount={amount}
                            balance={quote.data?.balance}
                            income={paid ? income.amount : undefined}
                            monthly={quote.data?.monthly}
                            onAllowDebt={setAllowDebt}
                            onAmount={(v) => {
                                setTouched(true)
                                setAmount(v)
                            }}
                        />
                    </>
                }
            />
            <FormFooter
                disabled={term.months + term.days <= 0 || !quote.data || (paid && (income.amount <= 0 || !income.date))}
                loading={m.isPending}
                onCancel={onDone}
                onSubmit={submit}
                submitIcon={paid ? <IconCheck size={16} /> : <IconCalendarPlus size={16} />}
                submitLabel={paid ? t('payment.commit') : t('extend.submit')}
            />
        </>
    )
}
