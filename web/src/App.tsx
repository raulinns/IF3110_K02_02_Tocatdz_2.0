import { Navigate, Route, Routes } from 'react-router-dom'
import { GuestOnly, RequireAdmin, RequireAuth } from './auth/guards.tsx'
import { AdminLayout } from './components/layout/AdminLayout.tsx'
import { AppLayout } from './components/layout/AppLayout.tsx'
import { DatabaseLayout } from './components/layout/DatabaseLayout.tsx'
import { ErrorPage } from './components/ui/ErrorPage.tsx'
import { ConnectionsPage } from './pages/admin/ConnectionsPage.tsx'
import { GrantsPage } from './pages/admin/GrantsPage.tsx'
import { GroupDetailPage } from './pages/admin/GroupDetailPage.tsx'
import { GroupsPage } from './pages/admin/GroupsPage.tsx'
import { UsersPage } from './pages/admin/UsersPage.tsx'
import { DatabasesPage } from './pages/databases/DatabasesPage.tsx'
import { RowsPage } from './pages/databases/RowsPage.tsx'
import { SchemaPage } from './pages/databases/SchemaPage.tsx'
import { TablePage } from './pages/databases/TablePage.tsx'
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
          <Route path="databases" element={<DatabasesPage />} />

          <Route path="databases/:db" element={<DatabaseLayout />}>
            <Route index element={<Navigate to="schema" replace />} />
            <Route path="schema" element={<SchemaPage />} />
            <Route path="schema/:schema/:table" element={<TablePage />} />
            <Route path="schema/:schema/:table/data" element={<RowsPage />} />
            <Route path="migrations" element={<Placeholder bare title="Migration" />} />
            <Route path="schema-diff" element={<Placeholder bare title="Schema diff" />} />
            <Route path="review" element={<Placeholder bare title="Review" />} />
            <Route path="jobs" element={<Placeholder bare title="Monitor job" />} />
            <Route path="history" element={<Placeholder bare title="Riwayat" />} />
          </Route>

          <Route element={<RequireAdmin />}>
            <Route path="admin" element={<AdminLayout />}>
              <Route index element={<Navigate to="users" replace />} />
              <Route path="users" element={<UsersPage />} />
              <Route path="groups" element={<GroupsPage />} />
              <Route path="groups/:groupId" element={<GroupDetailPage />} />
              <Route path="connections" element={<ConnectionsPage />} />
              <Route path="grants" element={<GrantsPage />} />
            </Route>
          </Route>

          <Route path="*" element={<ErrorPage status={404} />} />
        </Route>
      </Route>
    </Routes>
  )
}
