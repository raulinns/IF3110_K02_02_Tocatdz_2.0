import { Navigate, Route, Routes } from 'react-router-dom'
import { GuestOnly, RequireAdmin, RequireAuth } from './auth/guards.tsx'
import { AdminLayout } from './components/layout/AdminLayout.tsx'
import { AppLayout } from './components/layout/AppLayout.tsx'
import { DatabaseLayout } from './components/layout/DatabaseLayout.tsx'
import { ErrorPage } from './components/ui/ErrorPage.tsx'
import { LoginPage } from './pages/LoginPage.tsx'
import { Placeholder } from './pages/Placeholder.tsx'

export default function App() {
  return (
    <Routes>
      <Route element={<GuestOnly />}>
        <Route path="/login" element={<LoginPage />} />
      </Route>

      <Route element={<RequireAuth />}>
        <Route element={<AppLayout />}>
          <Route index element={<Navigate to="/databases" replace />} />
          <Route path="databases" element={<Placeholder title="Database" />} />

          <Route path="databases/:db" element={<DatabaseLayout />}>
            <Route index element={<Navigate to="schema" replace />} />
            <Route path="schema" element={<Placeholder bare title="Schema" />} />
            <Route path="schema/:schema/:table" element={<Placeholder bare title="Detail tabel" />} />
            <Route path="migrations" element={<Placeholder bare title="Migration" />} />
            <Route path="schema-diff" element={<Placeholder bare title="Schema diff" />} />
            <Route path="review" element={<Placeholder bare title="Review" />} />
            <Route path="jobs" element={<Placeholder bare title="Monitor job" />} />
            <Route path="history" element={<Placeholder bare title="Riwayat" />} />
          </Route>

          <Route element={<RequireAdmin />}>
            <Route path="admin" element={<AdminLayout />}>
              <Route index element={<Navigate to="users" replace />} />
              <Route path="users" element={<Placeholder bare title="User" />} />
              <Route path="groups" element={<Placeholder bare title="Group" />} />
              <Route path="connections" element={<Placeholder bare title="Koneksi" />} />
              <Route path="grants" element={<Placeholder bare title="Akses" />} />
            </Route>
          </Route>

          <Route path="*" element={<ErrorPage status={404} />} />
        </Route>
      </Route>
    </Routes>
  )
}
