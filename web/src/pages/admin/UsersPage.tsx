import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { updateUser, usersQuery } from '../../api/admin.ts'
import { errorMessage } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { useSession } from '../../auth/session-context.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { pageSearch, readPage } from '../../lib/pagination.ts'
import { CreateUserForm } from './CreateUserForm.tsx'
import { ResetPasswordDialog } from './ResetPasswordDialog.tsx'

type User = Schemas['User']
interface Message {
  kind: 'success' | 'error'
  text: string
}

export function UsersPage() {
  const client = useQueryClient()
  const session = useSession()
  const selfId = session.status === 'authenticated' ? session.session.user.id : ''
  const [params] = useSearchParams()
  const page = readPage(params)
  const query = useQuery(usersQuery(page))
  const [message, setMessage] = useState<Message | null>(null)
  const [deactivating, setDeactivating] = useState<User | null>(null)
  const [resetting, setResetting] = useState<User | null>(null)
  const [activatingId, setActivatingId] = useState<string | null>(null)
  const data = query.data

  const refresh = () => client.invalidateQueries({ queryKey: ['admin', 'users'] })

  async function activate(user: User) {
    setActivatingId(user.id)
    setMessage(null)
    try {
      await updateUser(user.id, { active: true })
      await refresh()
      setMessage({ kind: 'success', text: `User ${user.username} diaktifkan.` })
    } catch (error) {
      setMessage({ kind: 'error', text: errorMessage(error) })
    } finally {
      setActivatingId(null)
    }
  }

  async function deactivate(user: User) {
    await updateUser(user.id, { active: false })
    await refresh()
    setMessage({ kind: 'success', text: `User ${user.username} dinonaktifkan dan semua sesinya dicabut.` })
  }

  return (
    <div className="row g-4">
      <div className="col-xl-8">
        {message && <Notice kind={message.kind} onDismiss={() => setMessage(null)}>{message.text}</Notice>}
        {query.isPending && <BoxSkeleton rows={6} />}
        {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
        {data && (
          <>
            <div className="box box-primary mb-0" aria-busy={query.isPlaceholderData}>
              <div className="box-body">
                <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`}>
                  <table className="table table-hover" aria-label="Daftar user">
                    <thead>
                      <tr>
                        <th scope="col">Username</th>
                        <th scope="col">Status</th>
                        <th scope="col">Peran</th>
                        <th scope="col" className="text-end">
                          Tindakan
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.items.map((user) => {
                        const self = user.id === selfId
                        return (
                          <tr key={user.id}>
                            <td className="mono">
                              <i className="fas fa-user text-muted" aria-hidden="true" /> {user.username}
                              {self && <span className="text-muted small"> (Anda)</span>}
                            </td>
                            <td>{user.active ? <span className="badge status status-applied">Aktif</span> : <span className="badge status">Nonaktif</span>}</td>
                            <td>{user.platform_admin ? 'Admin platform' : 'User'}</td>
                            <td className="text-end">
                              <div className="d-inline-flex flex-wrap justify-content-end gap-2">
                                <button type="button" className="btn btn-sm btn-outline-secondary" aria-label="Reset password" title="Reset password" onClick={() => setResetting(user)}>
                                  <i className="fas fa-key" aria-hidden="true" />
                                  <span className="d-none d-md-inline">Reset password</span>
                                </button>
                                {user.active ? (
                                  <button
                                    type="button"
                                    className="btn btn-sm btn-outline-danger"
                                    aria-label="Nonaktifkan"
                                    disabled={self}
                                    title={self ? 'Akun sendiri tidak dapat dinonaktifkan' : 'Nonaktifkan'}
                                    onClick={() => setDeactivating(user)}
                                  >
                                    <i className="fas fa-user-slash" aria-hidden="true" />
                                    <span className="d-none d-md-inline">Nonaktifkan</span>
                                  </button>
                                ) : (
                                  <button type="button" className="btn btn-sm btn-outline-primary" aria-label="Aktifkan" title="Aktifkan" disabled={activatingId === user.id} onClick={() => void activate(user)}>
                                    <i className="fas fa-user-check" aria-hidden="true" />
                                    <span className="d-none d-md-inline">Aktifkan</span>
                                  </button>
                                )}
                              </div>
                            </td>
                          </tr>
                        )
                      })}
                      {data.items.length === 0 && (
                        <tr>
                          <td colSpan={4} className="text-muted">
                            {data.page > 1 ? (
                              <>
                                Tidak ada user di halaman ini. <Link to={{ search: '' }}>Kembali ke halaman pertama</Link>
                              </>
                            ) : (
                              'Belum ada user.'
                            )}
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
            {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={pageSearch} info={`Halaman ${data.page}`} label="Halaman user" />}
          </>
        )}
      </div>
      <div className="col-xl-4">
        <CreateUserForm
          onCreated={(user) => {
            void refresh()
            setMessage({ kind: 'success', text: `User ${user.username} berhasil ditambahkan.` })
          }}
        />
      </div>
      {deactivating && (
        <ConfirmDialog title="Nonaktifkan user" confirmLabel="Nonaktifkan" danger onConfirm={() => deactivate(deactivating)} onClose={() => setDeactivating(null)}>
          User <span className="mono">{deactivating.username}</span> tidak akan bisa masuk lagi dan semua sesi aktifnya dicabut. Akun bisa diaktifkan kembali kapan saja.
        </ConfirmDialog>
      )}
      {resetting && (
        <ResetPasswordDialog
          user={resetting}
          onDone={(user) => setMessage({ kind: 'success', text: `Password ${user.username} berhasil direset dan semua sesinya dicabut.` })}
          onClose={() => setResetting(null)}
        />
      )}
    </div>
  )
}
