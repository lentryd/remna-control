import { Center, Loader, Stack, Text } from '@mantine/core'

import { Logo } from '../logo'

// LoadingScreen fills the viewport (or a given height) while data loads.
export function LoadingScreen({ height = '100dvh', text }: { height?: string; text?: string; value?: number }) {
    const full = height === '100dvh'
    return (
        <Center style={{ height: full ? height : `max(240px, calc(${height} - 80px))` }}>
            <Stack align="center" gap="md">
                {full ? <Logo size={36} style={{ animation: 'remnac-pulse 1.4s ease-in-out infinite' }} /> : <Loader size="sm" />}
                {text && (
                    <Text c="dimmed" size="sm">
                        {text}
                    </Text>
                )}
            </Stack>
        </Center>
    )
}
