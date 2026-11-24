import { useQuery } from '@tanstack/react-query'
import { NavLink, Outlet, useParams } from 'react-router-dom'
import { databasesQuery } from '../../api/explorer.ts'
import type { Schemas } from '../../api/types.ts'
import { engineLabel } from '../../lib/labels.ts'
import { AccessBadge } from '../ui/AccessBadge.tsx'
import { ApiErrorView } from '../ui/ApiErrorView.tsx'
import { ErrorPage } from '../ui/ErrorPage.tsx'
import { PageHeader } from '../ui/PageHeader.tsx'
import { PageSkeleton } from '../ui/Skeleton.tsx'

type Capability = keyof Schemas['Database']['capabilities']

interface Tab {
  to: string
  icon: string
  label: string
  needs?: Capability
}

// Fitur lanjutan hanya ada untuk database yang mendukungnya sehingga tab lain disembunyikan, misalnya untuk MySQL
const tabs: Tab[] = [
  { to: 'schema', icon: 'fa-table', label: 'Schema' },
  { to: 'migrations', icon: 'fa-code-branch', label: 'Migration', needs: 'workflow' },
  { to: 'schema-diff', icon: 'fa-code-compare', label: 'Schema diff', needs: 'schema_diff' },
  { to: 'review', icon: 'fa-clipboard-check', label: 'Review', needs: 'workflow' },
  { to: 'jobs', icon: 'fa-gauge-high', label: 'Monitor', needs: 'workflow' },
  { to: 'history', icon: 'fa-clock-rotate-left', label: 'Riwayat', needs: 'workflow' },
]

// Kerangka semua halaman di bawah /databases/:db: breadcrumb, judul, dan tab bagian database
export function DatabaseLayout() {
  const { db = '' } = useParams()
  const query = useQuery(databasesQuery())

  if (query.isPending) return <PageSkeleton rows={3} />
  if (query.isError) return <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />
  const database = query.data.find((candidate) => candidate.id === db)
  // Database di luar daftar yang boleh diakses diperlakukan sama seperti ditolak supaya keberadaannya tidak terlihat
  if (!database) return <ErrorPage status={403} />

  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: 'Database', to: '/databases' }, { label: database.name }]}
        title={
          <>
            <span className="mono">{database.name}</span> <span className="badge badge-soft align-middle fs-6">{engineLabel(database.engine)}</span>{' '}
            <span className="align-middle fs-6">
              <AccessBadge access={database.access} />
            </span>
          </>
        }
      />
      <ul className="nav nav-tabs nav-tabs-scroll mb-3" aria-label="Bagian database">
        {tabs
          .filter((tab) => !tab.needs || database.capabilities[tab.needs])
          .map((tab) => (
            <li className="nav-item" key={tab.to}>
              <NavLink className="nav-link" to={tab.to}>
                <i className={`fas ${tab.icon}`} aria-hidden="true" /> {tab.label}
              </NavLink>
            </li>
          ))}
      </ul>
      <Outlet />
    </>
  )
}
