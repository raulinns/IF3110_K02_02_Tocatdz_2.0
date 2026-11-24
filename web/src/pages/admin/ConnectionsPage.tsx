import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { connectionsQuery, deleteConnection } from '../../api/admin.ts'
import type { Schemas } from '../../api/types.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { engineLabel } from '../../lib/labels.ts'
import { pageSearch, readPage } from '../../lib/pagination.ts'
import { CreateConnectionForm } from './CreateConnectionForm.tsx'

type Connection = Schemas['Connection']

export function ConnectionsPage() {
  const client = useQueryClient()
  const [params] = useSearchParams()
  const page = readPage(params)
  const query = useQuery(connectionsQuery(page))
  const [notice, setNotice] = useState<string | null>(null)
  const [removing, setRemoving] = useState<Connection | null>(null)
  const data = query.data

  // Daftar database di explorer dan akses ikut diperbarui karena sumbernya sama
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: ['admin'] }), client.invalidateQueries({ queryKey: ['databases'] })])

  async function remove(connection: Connection) {
    await deleteConnection(connection.id)
    await refresh()
    setNotice(`Koneksi ${connection.name} dihapus.`)
  }

  return (
    <div className="row g-4">
      <div className="col-xl-7">
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
                  <table className="table table-hover" aria-label="Daftar koneksi database">
                    <thead>
                      <tr>
                        <th scope="col">Nama</th>
                        <th scope="col">DBMS</th>
                        <th scope="col">ID</th>
                        <th scope="col" className="text-end">
                          Tindakan
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.items.map((connection) => (
                        <tr key={connection.id}>
                          <td className="mono">
                            <i className="fas fa-database text-muted" aria-hidden="true" /> {connection.name}
                          </td>
                          <td>{engineLabel(connection.engine)}</td>
                          <td className="mono text-muted small">{connection.id}</td>
                          <td className="text-end">
                            <button type="button" className="btn btn-sm btn-outline-danger" onClick={() => setRemoving(connection)}>
                              <i className="fas fa-trash" aria-hidden="true" />
                              Hapus
                            </button>
                          </td>
                        </tr>
                      ))}
                      {data.items.length === 0 && (
                        <tr>
                          <td colSpan={4} className="text-muted">
                            {data.page > 1 ? (
                              <>
                                Tidak ada koneksi di halaman ini. <Link to={{ search: '' }}>Kembali ke halaman pertama</Link>
                              </>
                            ) : (
                              'Belum ada koneksi.'
                            )}
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
            {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={pageSearch} info={`Halaman ${data.page}`} label="Halaman koneksi" />}
          </>
        )}
      </div>
      <div className="col-xl-5">
        <CreateConnectionForm
          onCreated={(connection) => {
            void refresh()
            setNotice(`Koneksi ${connection.name} berhasil disimpan.`)
          }}
        />
      </div>
      {removing && (
        <ConfirmDialog title="Hapus koneksi" confirmLabel="Hapus koneksi" danger onConfirm={() => remove(removing)} onClose={() => setRemoving(null)}>
          Koneksi <span className="mono">{removing.name}</span> dihapus dari platform beserta seluruh akses group ke database ini. Database aslinya tidak ikut terhapus.
        </ConfirmDialog>
      )}
    </div>
  )
}
