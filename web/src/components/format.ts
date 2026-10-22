import dayjs from 'dayjs'

import i18n from '@/app/i18n/i18n'

// Money is shown in the base currency (a setting) with the UI locale; the
// app root calls setFormat as soon as both are known, before any page
// renders, so the plain functions below can stay synchronous.
let base = 'RUB'
let locale = 'en-US'
let money0: Intl.NumberFormat
let money2: Intl.NumberFormat

function build() {
    const opts = { style: 'currency', currency: base, currencyDisplay: 'narrowSymbol' } as const
    money0 = new Intl.NumberFormat(locale, { ...opts, maximumFractionDigits: 0 })
    money2 = new Intl.NumberFormat(locale, { ...opts, maximumFractionDigits: 2, minimumFractionDigits: 2 })
}
build()

export function setFormat(opts: { base?: string; locale?: string }) {
    const b = opts.base || base
    const l = opts.locale || locale
    if (b === base && l === locale) return
    base = b
    locale = l
    build()
}

export const baseCurrency = () => base
export const fmtLocale = () => locale

// currencySymbol is the short sign of a currency (₽, €, $), or its code.
export const currencySymbol = (cur = base) =>
    new Intl.NumberFormat(locale, { style: 'currency', currency: cur, currencyDisplay: 'narrowSymbol' })
        .formatToParts(0)
        .find((p) => p.type === 'currency')?.value ?? cur

// fmtMoney with digits 0 drops the cents of whole amounts only: 120 → $120,
// 119.8 → $119.80 (never a lone tenth digit).
export function fmtMoney(v: number | null | undefined, digits: 0 | 2 = 0) {
    if (v === null || v === undefined) return '—'
    const cents = Math.round(v * 100)
    return (digits || cents % 100 !== 0 ? money2 : money0).format(cents / 100)
}

// numberSeparators are a locale's group and decimal signs, for inputs.
export function numberSeparators(loc: string) {
    const parts = new Intl.NumberFormat(loc).formatToParts(12345.6)
    return {
        thousandSeparator: parts.find((p) => p.type === 'group')?.value ?? ',',
        decimalSeparator: parts.find((p) => p.type === 'decimal')?.value ?? '.'
    }
}

export const fmtNum = (v: number, digits = 2) => new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(v)

export const fmtCurrency = (v: number, cur: string) => (cur === base ? fmtMoney(v, 2) : `${fmtNum(v)} ${cur}`)

export function fmtBytes(b: number | null | undefined): string {
    if (!b) return '0'
    const units = (['b', 'kb', 'mb', 'gb', 'tb'] as const).map((u) => i18n.t(`format.units.${u}`))
    let i = 0
    let v = b
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024
        i++
    }
    return `${fmtNum(v, v < 10 ? 2 : 1)} ${units[i]}`
}

// The panel's "forever" is an expiry in 2099; like the server (billing
// .UnlimitedYear) anything from 2090 on is an unlimited user.
export const UNLIMITED_YEAR = 2090
export const isUnlimited = (d: string | null | undefined) => !!d && dayjs(d).year() >= UNLIMITED_YEAR

// Date layouts follow the UI language (format.date / format.datetime).
export const dateLayout = () => i18n.t('format.date')
export const fmtDate = (d: string | null | undefined) => (!d ? '—' : isUnlimited(d) ? '∞' : dayjs(d).format(dateLayout()))
export const fmtDateTime = (d: string | null | undefined) => (!d ? '—' : isUnlimited(d) ? '∞' : dayjs(d).format(i18n.t('format.datetime')))
export const fromNow = (d: string | null | undefined) => (d ? dayjs(d).fromNow() : '—')

export function daysLeft(d: string | null | undefined): number | null {
    if (!d) return null
    if (isUnlimited(d)) return Infinity
    return Math.floor(dayjs(d).diff(dayjs(), 'hour') / 24)
}

export const durationLabel = (months: number, days: number) =>
    [months ? i18n.t('format.months_short', { count: months }) : '', days ? i18n.t('format.days_short', { count: days }) : '']
        .filter(Boolean)
        .join(' ') || '—'

export const GB = 1024 ** 3

// Traffic reset strategies of the panel.
export const STRATEGIES = ['NO_RESET', 'DAY', 'WEEK', 'MONTH', 'MONTH_ROLLING', 'SUBSCRIPTION'] as const

// trafficPct is the share of the traffic limit used, null without a limit.
export const trafficPct = (u: { used_traffic_bytes: number; traffic_limit_bytes: number } | null | undefined) =>
    u?.traffic_limit_bytes ? (u.used_traffic_bytes * 100) / u.traffic_limit_bytes : null

// TRAFFIC_LOW_PCT is where a limited user counts as running out of traffic.
export const TRAFFIC_LOW_PCT = 80

export const isTrafficLow = (u: { used_traffic_bytes: number; traffic_limit_bytes: number; status: string } | null | undefined) =>
    !!u && (u.status === 'LIMITED' || (trafficPct(u) ?? 0) >= TRAFFIC_LOW_PCT)

export const strategyLabel = (s: string) =>
    (STRATEGIES as readonly string[]).includes(s) ? i18n.t(`format.strategy.${s as (typeof STRATEGIES)[number]}`) : s
