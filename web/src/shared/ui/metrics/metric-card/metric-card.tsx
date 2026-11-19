import { Box, Card, Skeleton, Text } from '@mantine/core'
import type { ReactNode } from 'react'

import { fmtLocale } from '@/components/format'

import classes from './metric-card.module.css'

export interface IMetricCardProps {
    // semantic colour of the figure, for example red for a loss
    tone?: string
    isLoading?: boolean
    subtitle?: ReactNode
    title: string
    value: ReactNode
    rollingNumberComponent?: ReactNode
    // footer slot under the numbers: a sparkline, a meter
    children?: ReactNode
}

// MetricCardShared is a KPI tile: label, a large figure
// and a muted line of context.
export function MetricCardShared(props: IMetricCardProps) {
    const { tone, isLoading, title, value, subtitle, rollingNumberComponent, children } = props

    return (
        <Card className={classes.card} h="100%" padding="lg">
            <Text className={classes.title}>{title}</Text>
            {isLoading ? (
                <Skeleton h={30} mt={6} w={120} />
            ) : (
                (rollingNumberComponent ?? (
                    <Text c={tone} className={classes.value} component="div">
                        {typeof value === 'number' ? value.toLocaleString(fmtLocale()) : value}
                    </Text>
                ))
            )}
            {subtitle && (
                <Text className={classes.subtitle} component="div">
                    {subtitle}
                </Text>
            )}
            {children && <Box mt="md">{children}</Box>}
        </Card>
    )
}
