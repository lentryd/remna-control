import { ActionIcon, Box, Button, Group, Skeleton, Text, Tooltip } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useTranslation } from 'react-i18next'
import { IconKey, IconPlus, IconRefresh } from '@tabler/icons-react'

import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'
import { EntityAdd, EntityGrid } from '@shared/ui/entity-card'

import { ApiTokenItem } from './api-token-item'
import { isMobileViewport, useApiTokens } from './hooks'
import { CreateApiTokenContent } from './modals/create-api-token-modal'

function openCreate(title: string) {
    const isMobile = isMobileViewport()
    modals.open({
        title: <BaseOverlayHeader IconComponent={IconKey} title={title} />,
        fullScreen: isMobile,
        centered: true,
        size: 'min(800px, 90vw)',
        children: <CreateApiTokenContent isMobile={isMobile} />
    })
}

// ApiTokensCard lists the tokens other services use to call the API, one
// card each, with a tile to create another.
export function ApiTokensCard() {
    const { t } = useTranslation()
    const { data, isRefetching, refetch } = useApiTokens()
    const tokens = data?.tokens ?? []
    const create = () => openCreate(t('tokens.create'))

    return (
        <Box>
            <Group align="flex-start" justify="space-between" mb="md" wrap="nowrap">
                <Box miw={0}>
                    <Text c="var(--app-text-strong)" fw={600} fz={17} style={{ letterSpacing: '-0.01em' }}>
                        {t('settings.tokens')}
                    </Text>
                    <Text c="dimmed" size="sm">
                        {t('tokens.card_description')}
                    </Text>
                </Box>
                <Group gap="xs" wrap="nowrap">
                    <Tooltip label={t('tokens.refresh')}>
                        <ActionIcon aria-label={t('tokens.refresh')} loading={isRefetching} onClick={() => refetch()} size="lg" variant="default">
                            <IconRefresh size={16} />
                        </ActionIcon>
                    </Tooltip>
                    <Button leftSection={<IconPlus size={16} />} onClick={create} variant="filled">
                        {t('tokens.create')}
                    </Button>
                </Group>
            </Group>
            <EntityGrid min={280}>
                {!data && [0, 1].map((i) => <Skeleton h={200} key={i} radius="lg" />)}
                {tokens.map((token) => (
                    <ApiTokenItem key={token.id} token={token} />
                ))}
                {data && tokens.length === 0 && <EntityAdd label={t('tokens.none_cta')} onClick={create} />}
            </EntityGrid>
        </Box>
    )
}
