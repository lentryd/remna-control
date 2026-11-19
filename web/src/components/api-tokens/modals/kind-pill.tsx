// Adapted from remnawave/frontend (AGPL-3.0)
import { UnstyledButton } from '@mantine/core'
import clsx from 'clsx'
import { MouseEvent } from 'react'
import { IconCheck, IconMinus } from '@tabler/icons-react'

import classes from '../api-token-card.module.css'
import { KindState } from './scopes.utils'

interface IProps {
    label: string
    onClick?: () => void
    readOnly?: boolean
    state: KindState
}

export const KindPill = (props: IProps) => {
    const { label, onClick, readOnly, state } = props

    const handleClick = (event: MouseEvent) => {
        event.stopPropagation()
        onClick?.()
    }

    const isActive = state === 'on' || state === 'partial'

    return (
        <UnstyledButton
            className={clsx(classes.kindPill, {
                [classes.kindPillActive]: isActive
            })}
            data-readonly={readOnly || undefined}
            onClick={readOnly ? undefined : handleClick}
        >
            {state === 'on' && <IconCheck size={12} />}
            {state === 'partial' && <IconMinus size={12} />}
            {label}
        </UnstyledButton>
    )
}
