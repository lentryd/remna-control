import { defaultCssVariablesResolver, type CSSVariablesResolver } from '@mantine/core'

// App-level design tokens next to Mantine's: surfaces, borders and fills
// that components and CSS modules use instead of hard-coded colours.
export const cssVariablesResolver: CSSVariablesResolver = (theme) => {
    const base = defaultCssVariablesResolver(theme)
    // Mantine's "light" fills are opaque mixes; translucent tints sit better
    // on cards and stay subtle in the dark scheme.
    const tints = (shade: number, alpha: number, hover: number, text?: number) =>
        Object.fromEntries(
            Object.keys(theme.colors).flatMap((c) => [
                [`--mantine-color-${c}-light`, `color-mix(in srgb, var(--mantine-color-${c}-${shade}) ${alpha}%, transparent)`],
                [`--mantine-color-${c}-light-hover`, `color-mix(in srgb, var(--mantine-color-${c}-${shade}) ${hover}%, transparent)`],
                ...(text === undefined ? [] : [[`--mantine-color-${c}-light-color`, `var(--mantine-color-${c}-${text})`]])
            ])
        )
    return {
        variables: {
            ...base.variables,
            '--app-radius-card': theme.radius.lg,
            '--app-ease': 'cubic-bezier(0.2, 0, 0, 1)'
        },
        light: {
            ...base.light,
            ...tints(6, 9, 14),
            '--mantine-color-body': '#f6f6f7',
            '--mantine-color-text': '#18181b',
            '--mantine-color-dimmed': '#71717a',
            '--mantine-color-default': '#ffffff',
            '--mantine-color-default-hover': '#f4f4f5',
            '--mantine-color-default-border': '#e4e4e7',
            '--mantine-color-placeholder': '#a1a1aa',
            '--app-bg': '#f6f6f7',
            '--app-surface': '#ffffff',
            '--app-surface-2': '#fafafa',
            '--app-surface-raised': '#ffffff',
            '--app-border': '#e8e8eb',
            '--app-border-strong': '#d4d4d8',
            '--app-fill': 'rgb(9 9 11 / 0.04)',
            '--app-fill-hover': 'rgb(9 9 11 / 0.07)',
            '--app-row-hover': 'rgb(9 9 11 / 0.025)',
            '--app-text-strong': '#09090b',
            '--app-text-muted': '#71717a',
            '--app-text-faint': '#a1a1aa',
            '--app-shadow-card': '0 1px 2px rgb(9 9 11 / 0.04), 0 0 0 1px rgb(9 9 11 / 0.0)',
            '--app-shadow-pop': '0 16px 40px -12px rgb(9 9 11 / 0.18), 0 4px 10px -4px rgb(9 9 11 / 0.08)',
            '--app-overlay': 'rgb(9 9 11 / 0.35)',
            '--app-sidebar-bg': '#fbfbfb',
            '--app-glow': 'rgb(121 88 248 / 0.10)',
            // chart series (validated for CVD and contrast on the light surface)
            '--chart-income': '#7958f8',
            '--chart-expense': '#e8590c',
            '--chart-refund': '#12b886',
            '--chart-grid': '#ececef'
        },
        dark: {
            ...base.dark,
            ...tints(4, 13, 20, 3),
            '--mantine-color-body': '#09090b',
            '--mantine-color-text': '#ededef',
            '--mantine-color-dimmed': '#a1a1aa',
            '--mantine-color-default': '#18181b',
            '--mantine-color-default-hover': '#1f1f23',
            '--mantine-color-default-border': '#2c2c31',
            '--mantine-color-placeholder': '#63636b',
            '--app-bg': '#09090b',
            '--app-surface': '#111113',
            '--app-surface-2': '#151517',
            '--app-surface-raised': '#18181b',
            '--app-border': '#232327',
            '--app-border-strong': '#303036',
            '--app-fill': 'rgb(255 255 255 / 0.05)',
            '--app-fill-hover': 'rgb(255 255 255 / 0.08)',
            '--app-row-hover': 'rgb(255 255 255 / 0.025)',
            '--app-text-strong': '#fafafa',
            '--app-text-muted': '#a1a1aa',
            '--app-text-faint': '#6b6b74',
            '--app-shadow-card': '0 1px 0 rgb(255 255 255 / 0.03) inset',
            '--app-shadow-pop': '0 24px 48px -12px rgb(0 0 0 / 0.7), 0 0 0 1px rgb(255 255 255 / 0.05)',
            '--app-overlay': 'rgb(0 0 0 / 0.6)',
            '--app-sidebar-bg': '#0c0c0e',
            '--app-glow': 'rgb(131 99 249 / 0.18)',
            // chart series (validated for CVD and contrast on the dark surface)
            '--chart-income': '#8363f9',
            '--chart-expense': '#e8590c',
            '--chart-refund': '#12b886',
            '--chart-grid': '#222226'
        }
    }
}
