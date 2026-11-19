import { FocusTrap } from '@mantine/core'
import { modals } from '@mantine/modals'
import type { ReactNode } from 'react'
import { IconAlertTriangle } from '@tabler/icons-react'

import { BaseOverlayHeader } from '@shared/ui/overlays/base-overlay-header'
import { networkStore } from '@/api/client'
import i18n from '@/app/i18n/i18n'

export interface ModalHeader {
    color?: string
    icon: React.ComponentType<{ size: number }>
    subtitle?: ReactNode
    title: ReactNode
}

// openModal shows content in a modal titled with a tinted icon, title and
// subtitle; the render function gets a close callback.
export function openModal(header: ModalHeader, render: (close: () => void) => ReactNode, size: string | number = 'md') {
    const id = `m-${Math.random().toString(36).slice(2)}`
    const close = () => modals.close(id)
    modals.open({
        modalId: id,
        title: (
            <BaseOverlayHeader
                iconColor={header.color ?? 'brand'}
                IconComponent={header.icon}
                subtitle={header.subtitle}
                title={header.title}
            />
        ),
        size,
        // wide entity modals go full screen on phones
        fullScreen: size === '1000px' && window.matchMedia('(max-width: 40em)').matches,
        children: (
            <>
                <FocusTrap.InitialFocus />
                {render(close)}
            </>
        )
    })
    return close
}

export function confirmDanger(title: string, text: ReactNode, onConfirm: () => void, confirmLabel = i18n.t('common.delete')) {
    modals.openConfirmModal({
        title: <BaseOverlayHeader iconColor="red" IconComponent={IconAlertTriangle} title={title} />,
        children: (
            <>
                <FocusTrap.InitialFocus />
                {text}
            </>
        ),
        labels: { confirm: confirmLabel, cancel: i18n.t('common.cancel') },
        confirmProps: { color: 'red', variant: 'filled', disabled: networkStore.get().offline },
        cancelProps: { variant: 'default' },
        onConfirm
    })
}
