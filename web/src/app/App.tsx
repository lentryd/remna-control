import { useQueryClient } from '@tanstack/react-query'
import { lazy, useEffect } from 'react'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router'

import { setUnauthorizedHandler } from '@/api/client'
import { useMe, useSettings } from '@/api/hooks'
import { setFormat } from '@/components/format'
import { LoginPage } from '@/pages/LoginPage'

import { AuthLayout } from './layouts/auth'
import { MainLayout } from './layouts/main/main-layout'
import { LoadingScreen } from '@shared/ui/loading-screen'

// Pages load on demand, each in its own chunk; the layout's <Suspense> shows
// a progress bar meanwhile. The login page stays in the main bundle.
const CustomerPage = lazy(() => import('@/pages/CustomerPage').then((m) => ({ default: m.CustomerPage })))
const CompaniesPage = lazy(() => import('@/pages/CompaniesPage').then((m) => ({ default: m.CompaniesPage })))
const CompanyPage = lazy(() => import('@/pages/CompanyPage').then((m) => ({ default: m.CompanyPage })))
const CustomersPage = lazy(() => import('@/pages/CustomersPage').then((m) => ({ default: m.CustomersPage })))
const DashboardPage = lazy(() => import('@/pages/DashboardPage').then((m) => ({ default: m.DashboardPage })))
const ExpenseItemsPage = lazy(() => import('@/pages/ExpenseItemsPage').then((m) => ({ default: m.ExpenseItemsPage })))
const ExpensesPage = lazy(() => import('@/pages/ExpensesPage').then((m) => ({ default: m.ExpensesPage })))
const PaymentsPage = lazy(() => import('@/pages/PaymentsPage').then((m) => ({ default: m.PaymentsPage })))
const ReferralsPage = lazy(() => import('@/pages/ReferralsPage').then((m) => ({ default: m.ReferralsPage })))
const RwUsersPage = lazy(() => import('@/pages/RwUsersPage').then((m) => ({ default: m.RwUsersPage })))
const SettingsPage = lazy(() => import('@/pages/SettingsPage').then((m) => ({ default: m.SettingsPage })))
const SubscriptionsPage = lazy(() => import('@/pages/SubscriptionsPage').then((m) => ({ default: m.SubscriptionsPage })))
const TariffsPage = lazy(() => import('@/pages/TariffsPage').then((m) => ({ default: m.TariffsPage })))

export function App() {
    const navigate = useNavigate()
    const qc = useQueryClient()
    const location = useLocation()

    useEffect(() => {
        setUnauthorizedHandler(() => {
            qc.setQueryData(['me'], null)
            if (!window.location.hash.startsWith('#/login')) navigate('/login', { replace: true })
        })
    }, [navigate, qc])

    const me = useMe()
    const settings = useSettings(!!me.data)
    if (location.pathname === '/login') {
        return (
            <Routes>
                <Route element={<AuthLayout />}>
                    <Route path="/login" element={<LoginPage />} />
                </Route>
            </Routes>
        )
    }
    if (me.isPending || (me.data && settings.isPending)) {
        return <LoadingScreen />
    }
    if (!me.data) return <Navigate to="/login" replace />
    // money formatting follows the base currency; set before pages render
    setFormat({ base: settings.data?.base_currency })

    return (
        <Routes>
            <Route element={<MainLayout />}>
                <Route index element={<DashboardPage />} />
                <Route path="customers" element={<CustomersPage />} />
                <Route path="customers/:id" element={<CustomerPage />} />
                <Route path="companies" element={<CompaniesPage />} />
                <Route path="companies/:id" element={<CompanyPage />} />
                <Route path="subscriptions" element={<SubscriptionsPage />} />
                <Route path="payments" element={<PaymentsPage />} />
                <Route path="referrals" element={<ReferralsPage />} />
                <Route path="tariffs" element={<TariffsPage />} />
                <Route path="expenses" element={<ExpensesPage />} />
                <Route path="expense-items" element={<ExpenseItemsPage />} />
                <Route path="panel-users" element={<RwUsersPage />} />
                <Route path="settings" element={<SettingsPage />} />
                <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
        </Routes>
    )
}
