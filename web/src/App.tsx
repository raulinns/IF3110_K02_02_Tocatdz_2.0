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
          <Route path="databases" element={<Placeholder title="Database" task="FE-07" />} />

          <Route path="databases/:db" element={<DatabaseLayout />}>
            <Route index element={<Navigate to="schema" replace />} />
            <Route path="schema" element={<Placeholder bare title="Schema" task="FE-07" />} />
            <Route path="schema/:schema/:table" element={<Placeholder bare title="Detail tabel" task="FE-07" />} />
            <Route path="migrations" element={<Placeholder bare title="Migration" task="FE-08" />} />
            <Route path="schema-diff" element={<Placeholder bare title="Schema diff" task="FE-08" />} />
            <Route path="review" element={<Placeholder bare title="Review" task="FE-09" />} />
            <Route path="jobs" element={<Placeholder bare title="Monitor job" task="FE-10" />} />
            <Route path="history" element={<Placeholder bare title="Riwayat" task="FE-11" />} />
          </Route>

          <Route element={<RequireAdmin />}>
            <Route path="admin" element={<AdminLayout />}>
              <Route index element={<Navigate to="users" replace />} />
              <Route path="users" element={<Placeholder bare title="User" task="FE-06" />} />
              <Route path="groups" element={<Placeholder bare title="Group" task="FE-06" />} />
              <Route path="connections" element={<Placeholder bare title="Koneksi" task="FE-06" />} />
              <Route path="grants" element={<Placeholder bare title="Akses" task="FE-06" />} />
            </Route>
          </Route>

          <Route path="*" element={<ErrorPage status={404} />} />
        </Route>
      </Route>
    </Routes>
  )
}
