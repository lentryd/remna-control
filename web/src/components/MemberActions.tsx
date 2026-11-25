import { useTranslation } from 'react-i18next'

import { api } from '@/api/client'
import { useApiMutation } from '@/api/hooks'
import type { Company, CompanyMember } from '@/api/types'
import { notifyError, notifyOk } from '@/components/notify'
import { confirmDanger } from '@/modals/open'

// useMemberActions are the actions on one employee of a company, shared by
// the employees table and the employee card.
export function useMemberActions(company: Company) {
    const { t } = useTranslation()
    const call = useApiMutation((fn: () => Promise<unknown>) => fn())
    const run = (fn: () => Promise<unknown>, ok?: string) => call.mutate(fn, { onSuccess: () => ok && notifyOk(ok), onError: (e) => notifyError(e) })
    return {
        busy: call.isPending,
        remove: (m: CompanyMember) =>
            confirmDanger(
                t('companies.remove_member'),
                t('companies.remove_member_hint', { username: m.username }),
                () => run(() => api.del(`companies/${company.id}/members/${m.id}`), t('companies.member_removed')),
                t('companies.remove')
            ),
        toggleProtect: (m: CompanyMember) => run(() => api.put(`companies/${company.id}/members/${m.id}/protect`, { protected: !m.protected })),
        restore: (m: CompanyMember) => run(() => api.post(`companies/${company.id}/members`, { username: m.username, label: m.label }), t('companies.member_added'))
    }
}
