import { Badge, BadgeProps, type SelectProps } from '@mantine/core'
import { IconUsersGroup } from '@tabler/icons-react'

import { useExternalSquads, useSquads } from '@/api/hooks'
import { SearchSelect } from '../forms/search-select'
import i18n from '@/app/i18n/i18n'
import { useTranslation } from 'react-i18next'

export function useSquadName() {
    const squads = useSquads()
    return (uuid: string) => squads.data?.find((s) => s.uuid === uuid)?.name ?? i18n.t('infra.squad')
}

// SquadBadge shows an internal squad by name, as the panel's squad chips.
export function SquadBadge({ uuid, ...props }: { uuid: string } & Omit<BadgeProps, 'children'>) {
    const name = useSquadName()
    return (
        <Badge color="violet" leftSection={<IconUsersGroup size={14} />} variant="soft" w="fit-content" {...props}>
            {name(uuid)}
        </Badge>
    )
}

export function SquadSelect(props: Omit<SelectProps, 'data'>) {
    const { t } = useTranslation()
    const squads = useSquads()
    return (
        <SearchSelect
            data={(squads.data ?? []).map((s) => ({ value: s.uuid, label: `${s.name} · ${t('infra.members', { count: s.info.membersCount })}` }))}
            leftSection={<IconUsersGroup size={16} />}
            nothingFoundMessage={t('infra.no_squads')}
            {...props}
        />
    )
}

export function useExternalSquadName() {
    const squads = useExternalSquads()
    return (uuid: string) => squads.data?.find((s) => s.uuid === uuid)?.name ?? i18n.t('infra.external_squad')
}

// ExternalSquadSelect picks one external squad (the subscription page and
// related settings); clearing it gives null.
export function ExternalSquadSelect(props: Omit<SelectProps, 'data'>) {
    const { t } = useTranslation()
    const squads = useExternalSquads()
    const data = (squads.data ?? []).map((s) => ({ value: s.uuid, label: `${s.name} · ${t('infra.members', { count: s.info.membersCount })}` }))
    // A saved squad the panel no longer lists stays visible instead of looking unset.
    const current = typeof props.value === 'string' ? props.value : ''
    if (current && !squads.isPending && !data.some((d) => d.value === current)) {
        data.push({ value: current, label: `${t('infra.external_squad_missing')} · ${current}` })
    }
    return (
        <SearchSelect
            data={data}
            leftSection={<IconUsersGroup size={16} />}
            nothingFoundMessage={t('infra.no_external_squads')}
            clearable
            {...props}
        />
    )
}
