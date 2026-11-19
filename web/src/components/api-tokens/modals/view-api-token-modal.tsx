// Adapted from remnawave/frontend (AGPL-3.0): api-tokens-card/modals/view-api-token-modal.widget
import { ActionIcon, ActionIconGroup, Box, Button, CopyButton, FocusTrap, Stack, Text, Tooltip } from '@mantine/core'
import { modals } from '@mantine/modals'
import dayjs from 'dayjs'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { IconCheck, IconCopy } from '@tabler/icons-react'

import type { ApiToken } from '@/api/types'
import { CopyableCodeBlock } from '@shared/ui/copyable-code-block'
import { ModalFooter } from '@shared/ui/modal-footer'

import classes from '../api-token-card.module.css'
import { useTokenScopes } from '../hooks'
import { formatTokenTime } from '../time'
import { ScopeResourceRow } from './scope-resource-row'
import { countSelected, expandScopesToKeys } from './scopes.utils'

export function ViewApiTokenContent({ isMobile, token }: { isMobile: boolean; token: ApiToken }) {
    const { t } = useTranslation()
    const { data: scopesData } = useTokenScopes()
    const resources = useMemo(() => scopesData?.resources ?? [], [scopesData])
    const isExpired = !!token.expire_at && dayjs(token.expire_at).isBefore(dayjs())
    const [expanded, setExpanded] = useState<Set<string>>(new Set())

    const selectedEndpoints = useMemo(() => new Set(expandScopesToKeys(resources, token.scopes)), [resources, token.scopes])
    const grantedResources = resources.filter((r) => countSelected(r.endpoints, selectedEndpoints) > 0)

    const toggleExpand = (name: string) =>
        setExpanded((prev) => {
            const next = new Set(prev)
            if (next.has(name)) next.delete(name)
            else next.add(name)
            return next
        })

    return (
        <Stack>
            <FocusTrap.InitialFocus />
            <Stack gap="md" pb="xs">
                <Box className={classes.metaGrid}>
                    <div className={classes.metaCell}>
                        <Text className={classes.tokenColLabel}>{t('tokens.col_created')}</Text>
                        <Text className={classes.metaValue} fw={500}>
                            {formatTokenTime(token.created_at)}
                        </Text>
                    </div>
                    <div className={classes.metaCell}>
                        <Text className={classes.tokenColLabel}>{t('tokens.col_expires')}</Text>
                        <Text c={isExpired ? 'red' : undefined} className={classes.metaValue} fw={500}>
                            {token.expire_at ? formatTokenTime(token.expire_at) : t('tokens.never_expires')}
                        </Text>
                    </div>
                    <div className={classes.metaCell}>
                        <Text className={classes.tokenColLabel}>UUID</Text>
                        <div className={classes.metaUuid}>
                            <Text className={classes.metaUuidText} truncate="end">
                                {token.uuid}
                            </Text>
                            <CopyButton timeout={1600} value={token.uuid}>
                                {({ copied, copy }) => (
                                    <Tooltip label={t('common.copy')}>
                                        <ActionIcon color={copied ? 'teal' : 'gray'} onClick={copy} size="xs" variant="subtle">
                                            {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                                        </ActionIcon>
                                    </Tooltip>
                                )}
                            </CopyButton>
                        </div>
                    </div>
                </Box>

                {token.token && <CopyableCodeBlock value={token.token} />}

                <Stack gap={6}>
                    {grantedResources.map((resource) => (
                        <ScopeResourceRow
                            endpoints={resource.endpoints}
                            expanded={expanded.has(resource.resource)}
                            key={resource.resource}
                            onToggleExpand={() => toggleExpand(resource.resource)}
                            readOnly
                            resource={resource}
                            selectedEndpoints={selectedEndpoints}
                        />
                    ))}
                </Stack>
            </Stack>

            <ModalFooter isMobile={isMobile}>
                <ActionIconGroup ml="auto">
                    <CopyButton timeout={1600} value={JSON.stringify(token.scopes, null, 2)}>
                        {({ copied, copy }) => (
                            <Tooltip label={t('common.copy')}>
                                <ActionIcon color={copied ? 'teal' : 'gray'} onClick={copy} size="input-md" variant="soft">
                                    {copied ? <IconCheck size={24} /> : <IconCopy size={24} />}
                                </ActionIcon>
                            </Tooltip>
                        )}
                    </CopyButton>
                </ActionIconGroup>

                <Button onClick={() => modals.closeAll()} size="md" variant="filled">
                    {t('common.close')}
                </Button>
            </ModalFooter>
        </Stack>
    )
}
