import { IconCoins, IconCurrencyBitcoin, IconCurrencyDollar, IconCurrencyEuro, IconCurrencyPound, IconCurrencyRubel, IconCurrencyRupee, IconCurrencyTenge, IconCurrencyWon, IconCurrencyYen, IconCurrencyYuan, type IconProps } from '@tabler/icons-react'

import { baseCurrency } from '@/components/format'

// Currencies offered for expenses; any other ISO code can be typed in.
export const CURRENCIES = ['RUB', 'EUR', 'USD', 'GBP', 'CHF', 'CNY', 'JPY', 'TRY', 'KZT', 'BYN', 'UAH', 'AMD', 'GEL', 'PLN', 'CZK', 'INR', 'KRW']

// Base currencies need published rates: CBR for RUB, ECB for the rest.
export const BASE_CURRENCIES = ['RUB', 'EUR', 'USD', 'GBP', 'CHF', 'PLN', 'CZK', 'SEK', 'NOK', 'DKK', 'TRY', 'CNY', 'JPY', 'INR', 'KRW', 'BRL', 'CAD', 'AUD']

const icons: Record<string, typeof IconCoins> = {
    RUB: IconCurrencyRubel,
    EUR: IconCurrencyEuro,
    USD: IconCurrencyDollar,
    GBP: IconCurrencyPound,
    CNY: IconCurrencyYuan,
    JPY: IconCurrencyYen,
    INR: IconCurrencyRupee,
    KRW: IconCurrencyWon,
    KZT: IconCurrencyTenge,
    BTC: IconCurrencyBitcoin
}

// CurrencyIcon is the sign of a currency (the base one by default).
export function CurrencyIcon({ currency, ...props }: IconProps & { currency?: string }) {
    const Icon = icons[currency ?? baseCurrency()] ?? IconCoins
    return <Icon {...props} />
}
