import { Box, type BoxProps } from '@mantine/core'
import { nprogress } from '@mantine/nprogress'
import { forwardRef, type ReactNode, useEffect } from 'react'

interface PageProps extends BoxProps {
    children: ReactNode
    title: string
}

// Page sets the document title and fades the page in.
export const Page = forwardRef<HTMLDivElement, PageProps>(({ children, title, ...other }, ref) => {
    useEffect(() => {
        nprogress.complete()
        return () => nprogress.start()
    }, [])

    return (
        <>
            <title>{`${title} · Remna Control`}</title>
            <div className="remnac-enter">
                <Box ref={ref} {...other}>
                    {children}
                </Box>
            </div>
        </>
    )
})
