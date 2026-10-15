import { Box, Group, Text, type BoxProps } from '@mantine/core'

// Logo is the app mark: a flat two-tone shield.
export function Logo({ size = 26, ...props }: BoxProps & { size?: number }) {
    return (
        <Box component="svg" fill="none" h={size} viewBox="0 0 24 24" w={size} xmlns="http://www.w3.org/2000/svg" style={{ flexShrink: 0 }} {...props}>
            <path d="M12 2.2 20 5.1v6.1c0 5-3.3 8.9-8 10.6-4.7-1.7-8-5.6-8-10.6V5.1l8-2.9Z" fill="var(--logo-a, var(--mantine-color-brand-5))" />
            <path d="M12 2.2v19.6c4.7-1.7 8-5.6 8-10.6V5.1l-8-2.9Z" fill="var(--logo-b, var(--mantine-color-brand-7))" />
        </Box>
    )
}

// Wordmark is the logo with the product name.
export function Wordmark({ size = 24, compact }: { size?: number; compact?: boolean }) {
    return (
        <Group gap={9} wrap="nowrap">
            <Logo size={size} />
            {!compact && (
                <Text c="var(--app-text-strong)" fw={650} fz={15} lh={1} style={{ letterSpacing: '-0.02em', whiteSpace: 'nowrap' }}>
                    Remna Control
                </Text>
            )}
        </Group>
    )
}
