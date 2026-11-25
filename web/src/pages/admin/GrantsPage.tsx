import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { allConnectionsQuery, allGroupsQuery, grantsQuery, revokeGrant } from '../../api/admin.ts'
import type { Schemas } from '../../api/types.ts'
import { AccessBadge } from '../../components/ui/AccessBadge.tsx'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { accessLabel } from '../../lib/labels.ts'
import { pageSearch, readPage } from '../../lib/pagination.ts'
import { GrantForm, type GrantPreset } from './GrantForm.tsx'

type Grant = Schemas['Grant']

export function GrantsPage() {
  const client = useQueryClient()
  const [params] = useSearchParams()
  const page = readPage(params)
  const query = useQuery(grantsQuery(page))
  const groups = useQuery(allGroupsQuery())
  const connections = useQuery(allConnectionsQuery())
  const [notice, setNotice] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<Grant | null>(null)
  const [preset, setPreset] = useState<{ key: number; value: GrantPreset } | null>(null)
  const data = query.data

  // Daftar database di explorer ikut dimuat ulang karena akses menentukan isinya
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: ['admin', 'grants'] }), client.invalidateQueries({ queryKey: ['databases'] })])

  return (
    <>
      <Notice kind="info">Akses diberikan ke group, bukan langsung ke user. Jika user ada di beberapa group, akses tertinggi untuk database yang sama yang berlaku.</Notice>
      <div className="row g-4">
        <div className="col-xl-8">
          {notice && (
            <Notice kind="success" onDismiss={() => setNotice(null)}>
              {notice}
            </Notice>
          )}
          {query.isPending && <BoxSkeleton rows={5} />}
          {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
          {data && (
            <>
              <div className="box box-primary mb-0" aria-busy={query.isPlaceholderData}>
                <div className="box-body">
                  <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`}>
                    <table className="table table-hover" aria-label="Daftar akses database">
                      <thead>
                        <tr>
                          <th scope="col">Group</th>
                          <th scope="col">Database</th>
                          <th scope="col">Akses</th>
                          <th scope="col" className="text-end">
                            Tindakan
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {data.items.map((grant) => (
                          <tr key={`${grant.group_id}:${grant.database_id}`}>
                            <td>
                              <i className="fas fa-users text-muted" aria-hidden="true" /> {grant.group_name}
                            </td>
                            <td className="mono">{grant.database_name}</td>
                            <td>
                              <AccessBadge access={grant.access} />
                            </td>
                            <td className="text-end">
                              <div className="d-inline-flex flex-wrap justify-content-end gap-2">
                                <button
                                  type="button"
                                  className="btn btn-sm btn-outline-secondary"
                                  aria-label="Ubah"
                                  title="Ubah"
                                  onClick={() => setPreset({ key: (preset?.key ?? 0) + 1, value: { groupId: grant.group_id, databaseId: grant.database_id, access: grant.access } })}
                                >
                                  <i className="fas fa-pen" aria-hidden="true" />
                                  <span className="d-none d-md-inline">Ubah</span>
                                </button>
                                <button type="button" className="btn btn-sm btn-outline-danger" aria-label="Cabut" title="Cabut" onClick={() => setRevoking(grant)}>
                                  <i className="fas fa-ban" aria-hidden="true" />
                                  <span className="d-none d-md-inline">Cabut</span>
                                </button>
                              </div>
                            </td>
                          </tr>
                        ))}
                        {data.items.length === 0 && (
                          <tr>
                            <td colSpan={4} className="text-muted">
                              {data.page > 1 ? (
                                <>
                                  Tidak ada akses di halaman ini. <Link to={{ search: '' }}>Kembali ke halaman pertama</Link>
                                </>
                              ) : (
                                'Belum ada akses yang diberikan.'
                              )}
                            </td>
                          </tr>
                        )}
                      </tbody>
                    </table>
                  </div>
                </div>
              </div>
              {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={pageSearch} info={`Halaman ${data.page}`} label="Halaman akses" />}
            </>
          )}
        </div>
        <div className="col-xl-4">
          {groups.isError || connections.isError ? (
            <ApiErrorView
              error={groups.error ?? connections.error}
              onRetry={() => {
                void groups.refetch()
                void connections.refetch()
              }}
            />
          ) : groups.data && connections.data ? (
            <GrantForm
              key={preset?.key ?? 0}
              groups={groups.data}
              connections={connections.data}
              preset={preset?.value}
              onSaved={(grant) => {
                void refresh()
                setNotice(`Akses group ${grant.group_name} ke database ${grant.database_name} disimpan: ${accessLabel(grant.access)}.`)
              }}
            />
          ) : (
            <BoxSkeleton rows={3} />
          )}
        </div>
      </div>
      {revoking && (
        <ConfirmDialog
          title="Cabut akses"
          confirmLabel="Cabut akses"
          danger
          onConfirm={async () => {
            await revokeGrant(revoking.group_id, revoking.database_id)
            await refresh()
            setNotice(`Akses group ${revoking.group_name} ke database ${revoking.database_name} dicabut.`)
          }}
          onClose={() => setRevoking(null)}
        >
          Anggota group <span className="mono">{revoking.group_name}</span> tidak lagi bisa membuka database <span className="mono">{revoking.database_name}</span> lewat group ini.
        </ConfirmDialog>
      )}
    </>
  )
}
