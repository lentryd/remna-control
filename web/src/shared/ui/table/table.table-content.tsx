import { Box, type BoxProps, type ElementProps } from '@mantine/core'
import { forwardRef } from 'react'

import classes from './table.module.css'

export const DataTableContent = forwardRef<HTMLDivElement, BoxProps & ElementProps<'div', keyof BoxProps>>(({ children, className, ...props }, ref) => (
    <Box className={`${classes.content} remnac-table ${className ?? ''}`} ref={ref} {...props}>
        {children}
    </Box>
))
