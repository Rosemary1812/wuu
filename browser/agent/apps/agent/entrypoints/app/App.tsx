import type { FC } from 'react'
import { HashRouter, Navigate, Route, Routes } from 'react-router'
import { AuthLayout } from './layout/AuthLayout'
import { SidebarLayout } from './layout/SidebarLayout'
import { LoginPage } from './login/LoginPage'
import { LogoutPage } from './login/LogoutPage'
import { MagicLinkCallback } from './login/MagicLinkCallback'
import { ProfilePage } from './profile/ProfilePage'
import { WuuDesktopPage } from './wuu-desktop/WuuDesktopPage'

const WuuHomeRedirect: FC = () => <Navigate to="/home" replace />

export const App: FC = () => {
  return (
    <HashRouter>
      <Routes>
        <Route element={<AuthLayout />}>
          <Route path="login" element={<LoginPage />} />
          <Route path="logout" element={<LogoutPage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="auth/magic-link" element={<MagicLinkCallback />} />
        </Route>

        <Route element={<SidebarLayout />}>
          <Route path="home" element={<WuuDesktopPage />} />
        </Route>

        <Route path="/" element={<Navigate to="/home" replace />} />
        <Route path="home/*" element={<WuuHomeRedirect />} />
        <Route path="settings/*" element={<WuuHomeRedirect />} />
        <Route path="options/*" element={<WuuHomeRedirect />} />
        <Route path="onboarding/*" element={<WuuHomeRedirect />} />
        <Route path="connect-apps" element={<WuuHomeRedirect />} />
        <Route path="scheduled" element={<WuuHomeRedirect />} />
        <Route path="agents/*" element={<WuuHomeRedirect />} />
        <Route path="admin" element={<WuuHomeRedirect />} />
        <Route path="audit" element={<WuuHomeRedirect />} />
        <Route path="observability" element={<WuuHomeRedirect />} />
        <Route path="executions" element={<WuuHomeRedirect />} />
        <Route path="personalize" element={<WuuHomeRedirect />} />
        <Route path="*" element={<Navigate to="/home" replace />} />
      </Routes>
    </HashRouter>
  )
}
