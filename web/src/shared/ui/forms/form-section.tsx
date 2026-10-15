import { Box, Button, Card, Group, Stack, Text, ThemeIcon, ThemeIconProps } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { createContext, type ReactNode, useContext } from 'react'
import { PiFloppyDiskDuotone } from 'react-icons/pi'

import { ModalFooter } from '../modal-footer'
import { useTranslation } from 'react-i18next'

// Inside a FormStack sections appear one after another (.remnac-stagger
// delays), standalone they animate on their own.
const StaggerContext = createContext(false)

export function FormStack({ children, style }: { children: ReactNode; style?: React.CSSProperties }) {
    return (
        <StaggerContext.Provider value>
            <Stack className="remnac-stagger" gap="md" style={style}>
                {children}
            </Stack>
        </StaggerContext.Provider>
    )
}

// FormColumns is the wide (1000px) modal layout: two staggered columns
// that fold into one on narrow screens.
export function FormColumns({ left, right }: { left: ReactNode; right: ReactNode }) {
    return (
        <Group align="flex-start" gap="md" wrap="wrap">
            <FormStack style={{ flex: '1 1 400px' }}>{left}</FormStack>
            <FormStack style={{ flex: '1 1 400px' }}>{right}</FormStack>
        </Group>
    )
}

export function FormSection({
    icon: Icon,
    color = 'brand',
    title,
    description,
    actions,
    children
}: {
    icon: React.ComponentType<{ size: number }>
    color?: ThemeIconProps['color']
    title: string
    description?: ReactNode
    actions?: ReactNode
    children: ReactNode
}) {
    return (
        <div className="remnac-enter">
            <Card padding="md" radius="lg" style={{ boxShadow: 'none' }}>
                <Group align="flex-start" justify="space-between" mb="md" wrap="nowrap">
                    <Group align="flex-start" gap={10} miw={0} wrap="nowrap">
                        <ThemeIcon color={color} mt={1} radius="sm" size={24}>
                            <Icon size={14} />
                        </ThemeIcon>
                        <Box miw={0}>
                            <Text c="var(--app-text-strong)" fw={600} lh={1.4} size="sm">
                                {title}
                            </Text>
                            {description && (
                                <Text c="dimmed" component="div" lh={1.4} size="xs">
                                    {description}
                                </Text>
                            )}
                        </Box>
                    </Group>
                    {actions && <Box style={{ flexShrink: 0 }}>{actions}</Box>}
                </Group>
                <Stack gap="md">{children}</Stack>
            </Card>
        </div>
    )
}

// FieldGroup is a light, card-less group of fields for compact single-
// column forms: a small caption, then the fields; groups are split by a
// hairline.
export function FieldGroup({ title, description, children }: { title: string; description?: ReactNode; children: ReactNode }) {
    return (
        <Box className="field-group">
            <Text c="var(--app-text-strong)" fw={600} size="sm">
                {title}
            </Text>
            {description && (
                <Text c="dimmed" mt={2} size="xs">
                    {description}
                </Text>
            )}
            <Stack gap="md" mt="sm">
                {children}
            </Stack>
        </Box>
    )
}

// FormFooter is a form's button row: secondary actions, cancel, then the
// primary "save". In modals it sticks to the bottom edge; drawers (no
// Modal context) pass `inline`.
export function FormFooter({
    onCancel,
    loading,
    submitLabel,
    submitIcon,
    disabled,
    onSubmit,
    inline,
    children
}: {
    onCancel?: () => void
    loading?: boolean
    submitLabel?: string
    submitIcon?: ReactNode
    disabled?: boolean
    onSubmit?: () => void
    inline?: boolean
    children?: ReactNode
}) {
    const { t } = useTranslation()
    const isMobile = useMediaQuery('(max-width: 40em)') ?? false
    const inStack = useContext(StaggerContext)
    const buttons = (
        <>
            {children}
            {onCancel && (
                <Button onClick={onCancel} variant="default">
                    {t('common.cancel')}
                </Button>
            )}
            <Button
                disabled={disabled}
                leftSection={submitIcon ?? <PiFloppyDiskDuotone size={16} />}
                loading={loading}
                onClick={onSubmit}
                type={onSubmit ? 'button' : 'submit'}
                variant="filled"
            >
                {submitLabel ?? t('common.save')}
            </Button>
        </>
    )
    if (inline) {
        return (
            <Group gap="md" justify="flex-end" mt="md" wrap="wrap">
                {buttons}
            </Group>
        )
    }
    return (
        <ModalFooter isMobile={isMobile} mt={inStack ? 0 : 'md'}>
            {buttons}
        </ModalFooter>
    )
}
