// Adapted from remnawave/frontend (AGPL-3.0): api-tokens-card/modals/create-api-token-modal.widget
import { ActionIcon, ActionIconGroup, Box, Button, CopyButton, Flex, Group, NumberInput, Stack, Text, TextInput, Tooltip } from '@mantine/core'
import { useField } from '@mantine/form'
import { modals } from '@mantine/modals'
import { notifications } from '@mantine/notifications'
import { useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { TbAlertTriangle, TbBrandTelegram, TbCheck, TbClearAll, TbClipboard, TbCookie, TbCopy, TbEye, TbHexagon, TbWorld } from 'react-icons/tb'

import { api } from '@/api/client'
import { useApiMutation } from '@/api/hooks'
import type { ApiToken } from '@/api/types'
import { notifyError } from '@/components/notify'
import { ModalFooter } from '@shared/ui/modal-footer'
import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'

import classes from '../api-token-card.module.css'
import { useTokenScopes } from '../hooks'
import { ScopeResourceRow } from './scope-resource-row'
import { buildScopes, expandScopesToKeys, getKindState, getReadKeys, getWriteKeys, ScopeResource } from './scopes.utils'
import { ViewApiTokenContent } from './view-api-token-modal'

const DEFAULT_EXPIRES_IN_DAYS = 30

// What subpage needs to read the add-on catalog.
const SUBPAGE_PRESET_KEYS = ['addons:list']

// What remna-control-bot needs: find/create customers, link a Telegram id,
// read their card (subscriptions, balance, referrals) and the tariffs;
// the two lists are for notifications (traffic, payment reminders); the
// rest takes payments: quote and extend items, record payments, provision
// subscriptions, connect add-ons and change tariffs, and the base currency
// from the settings (`stats:settings`, read only).
const BOT_PRESET_KEYS = [
    'customers:lookup',
    'customers:get',
    'customers:create',
    'customers:update',
    'customers:list',
    'customers:payment_preview',
    'customers:pay',
    'subscriptions:list',
    'subscriptions:provision',
    'subscriptions:connect_addon',
    'items:quote',
    'items:extend',
    'items:tariff_quote',
    'items:tariff',
    'tariffs:list',
    'stats:settings'
]

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

export function CreateApiTokenContent({ isMobile }: { isMobile: boolean }) {
    const { t } = useTranslation()
    const { data: scopesData } = useTokenScopes()

    const nameField = useField({
        initialValue: '',
        validateOnChange: true,
        validate: (value: string) => (value.trim() ? null : t('tariffs.name_required'))
    })

    const [selectedEndpoints, setSelectedEndpoints] = useState<Set<string>>(new Set())
    const [expanded, setExpanded] = useState<Set<string>>(new Set())
    const [expiresInDays, setExpiresInDays] = useState<number | string>(DEFAULT_EXPIRES_IN_DAYS)

    const resources = scopesData?.resources ?? []

    const create = useApiMutation((body: { name: string; expires_in_days: number; scopes: string[] }) => api.post<ApiToken>('api-tokens', body))

    const setKeys = (keys: string[], checked: boolean) =>
        setSelectedEndpoints((prev) => {
            const next = new Set(prev)
            keys.forEach((key) => (checked ? next.add(key) : next.delete(key)))
            return next
        })

    const toggleEndpoint = (key: string) =>
        setSelectedEndpoints((prev) => {
            const next = new Set(prev)
            if (next.has(key)) next.delete(key)
            else next.add(key)
            return next
        })

    const toggleKind = (resource: ScopeResource, kind: 'read' | 'write') => {
        const keys = kind === 'read' ? getReadKeys(resource) : getWriteKeys(resource)
        setKeys(keys, getKindState(keys, selectedEndpoints) !== 'on')
    }

    const toggleExpand = (name: string) =>
        setExpanded((prev) => {
            const next = new Set(prev)
            if (next.has(name)) next.delete(name)
            else next.add(name)
            return next
        })

    const presetRead = () => setSelectedEndpoints(new Set(resources.flatMap((r) => getReadKeys(r))))
    const presetFull = () => setSelectedEndpoints(new Set(resources.flatMap((r) => r.endpoints.map((e) => e.key))))
    const presetSubpage = () => setSelectedEndpoints(new Set(SUBPAGE_PRESET_KEYS))
    const presetBot = () => setSelectedEndpoints(new Set(BOT_PRESET_KEYS))

    const handlePasteScopes = async () => {
        try {
            const parsed: unknown = JSON.parse(await navigator.clipboard.readText())
            if (!Array.isArray(parsed) || !parsed.every((item) => typeof item === 'string')) throw new Error('not a string array')
            setSelectedEndpoints(new Set(expandScopesToKeys(resources, parsed as string[])))
            notifications.show({ title: t('tokens.pasted'), message: t('tokens.pasted_message', { count: parsed.length }), color: 'teal' })
        } catch {
            notifications.show({ title: t('tokens.invalid_scopes'), message: t('tokens.invalid_scopes_message'), color: 'red' })
        }
    }

    const handleSubmit = () =>
        create.mutate(
            { name: nameField.getValue().trim(), expires_in_days: Number(expiresInDays), scopes: buildScopes(resources, selectedEndpoints) },
            {
                onSuccess: async (data) => {
                    modals.closeAll()
                    await sleep(300)
                    modals.open({
                        title: <BaseOverlayHeader iconColor="teal" IconComponent={TbCookie} iconVariant="soft" title={data.name} />,
                        fullScreen: isMobile,
                        centered: true,
                        size: 'min(800px, 90vw)',
                        children: <ViewApiTokenContent isMobile={isMobile} token={data} />
                    })
                },
                onError: (e) => notifyError(e)
            }
        )

    const totalSelected = selectedEndpoints.size
    const isNameInvalid = !!nameField.error || nameField.getValue().trim() === ''
    const isExpiresInvalid = !Number.isInteger(Number(expiresInDays)) || Number(expiresInDays) < 1
    const canCreate = !isNameInvalid && !isExpiresInvalid && totalSelected > 0

    return (
        <Stack gap="md">
            <Box className={classes.oneTimeNotice}>
                <TbAlertTriangle className={classes.oneTimeNoticeIcon} size={20} />
                <Text className={classes.oneTimeNoticeText}>
                    <Trans components={{ emphasis: <span className={classes.oneTimeNoticeEmphasis} /> }} i18nKey="tokens.one_time_notice" />
                </Text>
            </Box>

            <Flex align={isMobile ? 'stretch' : 'flex-start'} direction={isMobile ? 'column' : 'row'} gap="xs">
                <TextInput
                    data-autofocus
                    flex={isMobile ? undefined : 1}
                    label={t('tokens.token_name')}
                    placeholder="subpage"
                    required
                    {...nameField.getInputProps()}
                />
                <NumberInput
                    allowDecimal={false}
                    allowNegative={false}
                    clampBehavior="strict"
                    label={t('tokens.expires_in_days')}
                    max={999999}
                    min={1}
                    onChange={setExpiresInDays}
                    required
                    value={expiresInDays}
                    w={isMobile ? '100%' : 300}
                />
            </Flex>

            <Group gap="xs">
                <Button leftSection={<TbEye size={16} />} onClick={presetRead} size="xs" variant="default">
                    {t('tokens.read_only')}
                </Button>
                <Button leftSection={<TbWorld size={16} />} onClick={presetFull} size="xs" variant="default">
                    {t('tokens.full_access')}
                </Button>
                <Button leftSection={<TbHexagon size={16} />} onClick={presetSubpage} size="xs" variant="default">
                    Subpage
                </Button>
                <Button leftSection={<TbBrandTelegram size={16} />} onClick={presetBot} size="xs" variant="default">
                    Telegram bot
                </Button>
            </Group>

            <Stack gap={6}>
                {resources.map((resource) => (
                    <ScopeResourceRow
                        endpoints={resource.endpoints}
                        expanded={expanded.has(resource.resource)}
                        key={resource.resource}
                        onToggleEndpoint={toggleEndpoint}
                        onToggleExpand={() => toggleExpand(resource.resource)}
                        onToggleKind={(kind) => toggleKind(resource, kind)}
                        resource={resource}
                        selectedEndpoints={selectedEndpoints}
                    />
                ))}
            </Stack>

            <ModalFooter isMobile={isMobile}>
                <ActionIconGroup ml="auto">
                    <Tooltip label={t('tokens.clear')}>
                        <ActionIcon color="gray" onClick={() => setSelectedEndpoints(new Set())} size="input-md" variant="soft">
                            <TbClearAll size={24} />
                        </ActionIcon>
                    </Tooltip>

                    <CopyButton timeout={1600} value={JSON.stringify(buildScopes(resources, selectedEndpoints), null, 2)}>
                        {({ copied, copy }) => (
                            <Tooltip label={t('common.copy')}>
                                <ActionIcon color={copied ? 'teal' : 'gray'} onClick={copy} size="input-md" variant="soft">
                                    {copied ? <TbCheck size={24} /> : <TbCopy size={24} />}
                                </ActionIcon>
                            </Tooltip>
                        )}
                    </CopyButton>

                    <Tooltip label={t('tokens.paste')}>
                        <ActionIcon color="gray" onClick={handlePasteScopes} size="input-md" variant="soft">
                            <TbClipboard size={24} />
                        </ActionIcon>
                    </Tooltip>
                </ActionIconGroup>

                <Button
                    disabled={!canCreate}
                    leftSection={<TbCookie size="24px" />}
                    loading={create.isPending}
                    onClick={handleSubmit}
                    size="md"
                    variant="filled"
                >
                    {t('common.create')}
                </Button>
            </ModalFooter>
        </Stack>
    )
}
