import { Alert, Badge, Button, Group, SimpleGrid, Stack, Text } from '@mantine/core'
import { IconAlertTriangle, IconLock, IconLockOpen, IconTag, IconTrash, IconUser, IconUserCheck } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import { useCompanyMembers } from '@/api/hooks'
import type { Company, CompanyMember } from '@/api/types'
import { fmtBytes, fmtDate, strategyLabel } from '@/components/format'
import { useMemberActions } from '@/components/MemberActions'
import { OverrideBadges, SegmentHistory } from '@/components/segments'
import { Money } from '@/components/ui'
import { LoadingScreen } from '@shared/ui/loading-screen'

import { openMemberOverrides } from './CompanyModals'
import { openModal } from './open'
import { IdentityPanel } from './ViewItemModal'

// openMemberModal is the employee card: the panel user, what the employee is
// billed on now, the actions, and the billing history by stretch of days.
export function openMemberModal(company: Company, member: Pick<CompanyMember, 'id' | 'username' | 'label'>) {
    openModal(
        { icon: IconUser, title: member.username, subtitle: member.label || company.name },
        () => <MemberCard company={company} id={member.id} />,
        '1000px'
    )
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div>
            <Text c="dimmed" size="xs">
                {label}
            </Text>
            <Text component="div" size="sm">
                {children}
            </Text>
        </div>
    )
}

function MemberCard({ company, id }: { company: Company; id: number }) {
    const { t } = useTranslation()
    const { data } = useCompanyMembers(company.id)
    const actions = useMemberActions(company)
    const m = data?.find((x) => x.id === id)
    if (!data) return <LoadingScreen height="40vh" />
    if (!m) {
        return (
            <Text c="dimmed" p="md">
                {t('errors.company.not_found')}
            </Text>
        )
    }
    return (
        <Stack gap="md">
            <Group gap="xs">
                {!m.active && (
                    <Badge color="gray" variant="soft">
                        {t('companies.removed_on', { date: fmtDate(m.removed_at) })}
                    </Badge>
                )}
                {m.protected && (
                    <Badge color="brand" leftSection={<IconLock size={12} />} variant="soft">
                        {t('companies.protected')}
                    </Badge>
                )}
                {m.tariff_pinned && (
                    <Badge color="orange" variant="soft">
                        {t('companies.pinned')}
                    </Badge>
                )}
            </Group>

            {m.panel_deleted && (
                <Alert color="orange" icon={<IconAlertTriangle size={18} />}>
                    {t('companies.panel_deleted')}
                </Alert>
            )}

            <IdentityPanel rw={m.rw_user} />

            <SimpleGrid cols={{ base: 2, sm: 4 }}>
                <Fact label={t('companies.added')}>{fmtDate(m.added_at)}</Fact>
                <Fact label={t('companies.tariff')}>{m.tariff_name}</Fact>
                <Fact label={t('companies.price_month')}>
                    <Money digits={2} value={m.price} />
                </Fact>
                <Fact label={t('tariffs.traffic_limit')}>
                    {m.traffic_limit_bytes ? fmtBytes(m.traffic_limit_bytes) : '∞'} · {strategyLabel(m.traffic_strategy)}
                </Fact>
            </SimpleGrid>
            {m.overrides.length > 0 && (
                <Group gap={6}>
                    <Text c="dimmed" size="xs">
                        {t('companies.overrides')}
                    </Text>
                    <OverrideBadges fields={m.overrides} />
                </Group>
            )}

            <Group gap="xs">
                {m.active ? (
                    <>
                        <Button disabled={company.archived} leftSection={<IconTag size={16} />} onClick={() => openMemberOverrides(company, m)} variant="default">
                            {t('companies.member_tariff')}
                        </Button>
                        <Button
                            disabled={company.archived}
                            leftSection={m.protected ? <IconLockOpen size={16} /> : <IconLock size={16} />}
                            loading={actions.busy}
                            onClick={() => actions.toggleProtect(m)}
                            variant="default"
                        >
                            {m.protected ? t('companies.unprotect') : t('companies.protect')}
                        </Button>
                        <Button color="red" disabled={company.archived || m.protected} leftSection={<IconTrash size={16} />} onClick={() => actions.remove(m)} variant="light">
                            {m.protected ? t('companies.protected_cant_remove') : t('companies.remove')}
                        </Button>
                    </>
                ) : (
                    <Button disabled={company.archived} leftSection={<IconUserCheck size={16} />} loading={actions.busy} onClick={() => actions.restore(m)} variant="default">
                        {t('companies.restore')}
                    </Button>
                )}
            </Group>

            <SegmentHistory segments={m.segments} />
        </Stack>
    )
}
