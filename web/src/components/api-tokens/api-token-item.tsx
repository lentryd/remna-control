import { Badge, Group, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'
import { IconActivity, IconCalendarPlus, IconEye, IconKey, IconShieldCheck, IconTrash } from '@tabler/icons-react'

import { api } from '@/api/client'
import { useApiMutation } from '@/api/hooks'
import type { ApiToken } from '@/api/types'
import { daysLeft, fmtDate, fmtDateTime, fromNow } from '@/components/format'
import { confirmDanger } from '@/modals/open'
import { notifyError } from '@/components/notify'
import { EntityCard, EntityParam } from '@shared/ui/entity-card'
import { FooterFigure } from '@shared/ui/entity-card/footer-figure'
import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'

import { isMobileViewport } from './hooks'
import { ViewApiTokenContent } from './modals/view-api-token-modal'

// ApiTokenItem is one token: what it may call, when it lapses and when it
// was last used. A click shows its scopes.
export function ApiTokenItem({ token }: { token: ApiToken }) {
    const { t } = useTranslation()
    const remove = useApiMutation(() => api.del(`api-tokens/${token.id}`))

    const isFull = token.scopes.includes('*')
    const isExpired = !!token.expire_at && dayjs(token.expire_at).isBefore(dayjs())
    const left = daysLeft(token.expire_at)
    const expiryColor = isExpired ? 'red' : left !== null && left <= 7 ? 'orange' : undefined

    const view = () => {
        const isMobile = isMobileViewport()
        modals.open({
            title: <BaseOverlayHeader iconColor="teal" IconComponent={IconKey} iconVariant="soft" title={token.name} />,
            fullScreen: isMobile,
            centered: true,
            size: 'min(800px, 90vw)',
            children: <ViewApiTokenContent isMobile={isMobile} token={token} />
        })
    }
    const del = () =>
        confirmDanger(t('tokens.delete', { name: token.name }), t('tokens.delete_hint'), () =>
            remove.mutate(undefined, { onError: (e) => notifyError(e) })
        )

    return (
        <EntityCard
            badges={isExpired && <Badge color="red">{t('tokens.expired')}</Badge>}
            dimmed={isExpired}
            dot={isExpired ? 'red' : isFull ? 'teal' : 'brand'}
            footer={
                <>
                    <FooterFigure label={t('tokens.col_expires')}>
                        <Text c={expiryColor} component="span" inherit>
                            {token.expire_at ? fmtDate(token.expire_at) : t('tokens.never_expires')}
                        </Text>
                    </FooterFigure>
                    {token.expire_at && !isExpired && (
                        <Text c={expiryColor ?? 'dimmed'} size="xs">
                            {fromNow(token.expire_at)}
                        </Text>
                    )}
                </>
            }
            menu={[
                { label: t('tokens.view'), icon: IconEye, onClick: view },
                { label: t('common.delete'), icon: IconTrash, color: 'red', onClick: del, disabled: remove.isPending }
            ]}
            onClick={view}
            params={
                <>
                    <EntityParam icon={IconCalendarPlus} label={t('tokens.col_created')}>
                        {fmtDateTime(token.created_at)}
                    </EntityParam>
                    <EntityParam icon={IconActivity} label={t('tokens.last_used')}>
                        {token.last_used_at ? (
                            fromNow(token.last_used_at)
                        ) : (
                            <Text c="dimmed" component="span" inherit>
                                {t('tokens.never_used')}
                            </Text>
                        )}
                    </EntityParam>
                </>
            }
            subtitle={
                <Text c="dimmed" ff="monospace" inherit>
                    {token.prefix}…
                </Text>
            }
            title={token.name}
        >
            <Group gap={6} mt="md">
                {isFull ? (
                    <Badge color="teal" leftSection={<IconShieldCheck size={12} />} size="lg">
                        {t('tokens.full_access')}
                    </Badge>
                ) : (
                    <Badge color="brand" size="lg">
                        {t('tokens.endpoints', { count: token.scopes.length })}
                    </Badge>
                )}
            </Group>
        </EntityCard>
    )
}
