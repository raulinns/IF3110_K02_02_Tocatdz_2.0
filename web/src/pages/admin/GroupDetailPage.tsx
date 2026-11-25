import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { allGroupsQuery, allUsersQuery, deleteGroup, groupMembersQuery, removeMember, setLeader } from '../../api/admin.ts'
import type { Schemas } from '../../api/types.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Breadcrumbs } from '../../components/ui/Breadcrumbs.tsx'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { ErrorPage } from '../../components/ui/ErrorPage.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { AddMemberForm } from './AddMemberForm.tsx'
import { RenameGroupDialog } from './RenameGroupDialog.tsx'

type User = Schemas['User']
interface Message {
  kind: 'success' | 'error'
  text: string
}

export function GroupDetailPage() {
  const { groupId = '' } = useParams()
  const navigate = useNavigate()
  const client = useQueryClient()
  const groups = useQuery(allGroupsQuery())
  const group = groups.data?.find((candidate) => candidate.id === groupId)
  const members = useQuery({ ...groupMembersQuery(groupId), enabled: group !== undefined })
  const users = useQuery(allUsersQuery())
  const [message, setMessage] = useState<Message | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleted, setDeleted] = useState(false)
  const [removing, setRemoving] = useState<User | null>(null)
  const [promoting, setPromoting] = useState<User | null>(null)

  const refresh = () => client.invalidateQueries({ queryKey: ['admin', 'groups'] })

  // Setelah dihapus halaman ini tidak merender apa pun sampai perpindahan ke daftar selesai supaya 404 tidak sempat tampil
  if (deleted) return null
  if (groups.isPending) return <BoxSkeleton rows={5} />
  if (groups.isError) return <ApiErrorView error={groups.error} onRetry={() => void groups.refetch()} />
  if (!group) return <ErrorPage status={404} />

  const list = members.data ?? []
  const leader = list.find((member) => member.id === group.leader_user_id)
  const memberIds = new Set(list.map((member) => member.id))

  return (
    <>
      <Breadcrumbs items={[{ label: 'Group', to: '/admin/groups' }, { label: group.name }]} />
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
        <div>
          <h2 className="h5 mb-1">
            <i className="fas fa-users text-muted" aria-hidden="true" /> {group.name}
          </h2>
          <div className="text-muted small">
            {group.member_count} anggota · Leader: {leader ? <span className="mono">{leader.username}</span> : 'belum ada'}
          </div>
        </div>
        <div className="d-flex gap-2">
          <button type="button" className="btn btn-sm btn-outline-secondary" onClick={() => setRenaming(true)}>
            <i className="fas fa-pen" aria-hidden="true" />
            Ganti nama
          </button>
          <button type="button" className="btn btn-sm btn-outline-danger" onClick={() => setDeleting(true)}>
            <i className="fas fa-trash" aria-hidden="true" />
            Hapus group
          </button>
        </div>
      </div>
      {message && (
        <Notice kind={message.kind} onDismiss={() => setMessage(null)}>
          {message.text}
        </Notice>
      )}
      {!group.leader_user_id && (
        <Notice kind="warning">
          Group ini belum memiliki leader, sehingga migration yang melibatkan group ini belum bisa diproses. {list.length === 0 ? 'Tambahkan anggota lebih dulu, lalu pilih salah satunya sebagai leader.' : 'Pilih salah satu anggota aktif sebagai leader.'}
        </Notice>
      )}
      <div className="row g-4">
        <div className="col-xl-8">
          {members.isPending && <BoxSkeleton rows={4} />}
          {members.isError && <ApiErrorView error={members.error} onRetry={() => void members.refetch()} />}
          {members.data && (
            <div className="box box-primary mb-0">
              <div className="box-body">
                <div className="table-responsive">
                  <table className="table table-hover" aria-label={`Anggota group ${group.name}`}>
                    <thead>
                      <tr>
                        <th scope="col">Anggota</th>
                        <th scope="col" className="text-end">
                          Tindakan
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {list.map((member) => {
                        const isLeader = member.id === group.leader_user_id
                        return (
                          <tr key={member.id}>
                            <td>
                              <span className="mono">
                                <i className="fas fa-user text-muted" aria-hidden="true" /> {member.username}
                              </span>{' '}
                              {isLeader && <span className="badge status status-applied">Leader</span>}{' '}
                              {!member.active && <span className="badge status">Nonaktif</span>}
                            </td>
                            <td className="text-end">
                              <div className="d-inline-flex flex-wrap justify-content-end gap-2">
                                {!isLeader && (
                                  <button
                                    type="button"
                                    className="btn btn-sm btn-outline-primary"
                                    aria-label="Jadikan leader"
                                    title={member.active ? 'Jadikan leader' : 'User nonaktif tidak dapat menjadi leader'}
                                    disabled={!member.active}
                                    onClick={() => setPromoting(member)}
                                  >
                                    <i className="fas fa-crown" aria-hidden="true" />
                                    <span className="d-none d-md-inline">Jadikan leader</span>
                                  </button>
                                )}
                                <button
                                  type="button"
                                  className="btn btn-sm btn-outline-danger"
                                  aria-label="Keluarkan"
                                  title={isLeader ? 'Ganti leader sebelum mengeluarkan anggota ini' : 'Keluarkan'}
                                  disabled={isLeader}
                                  onClick={() => setRemoving(member)}
                                >
                                  <i className="fas fa-user-minus" aria-hidden="true" />
                                  <span className="d-none d-md-inline">Keluarkan</span>
                                </button>
                              </div>
                            </td>
                          </tr>
                        )
                      })}
                      {list.length === 0 && (
                        <tr>
                          <td colSpan={2} className="text-muted">
                            Belum ada anggota.
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )}
        </div>
        <div className="col-xl-4">
          {users.isError ? (
            <ApiErrorView error={users.error} onRetry={() => void users.refetch()} />
          ) : (
            <AddMemberForm
              groupId={group.id}
              users={users.data ?? []}
              memberIds={memberIds}
              onAdded={(user) => {
                void refresh()
                setMessage({ kind: 'success', text: `${user.username} ditambahkan ke group ${group.name}.` })
              }}
            />
          )}
        </div>
      </div>
      {renaming && (
        <RenameGroupDialog
          group={group}
          onDone={(updated) => {
            void refresh()
            setMessage({ kind: 'success', text: `Nama group diubah menjadi ${updated.name}.` })
          }}
          onClose={() => setRenaming(false)}
        />
      )}
      {promoting && (
        <ConfirmDialog
          title="Ganti leader"
          confirmLabel="Jadikan leader"
          onConfirm={async () => {
            await setLeader(group.id, promoting.id)
            await refresh()
            setMessage({ kind: 'success', text: `${promoting.username} sekarang menjadi leader group ${group.name}.` })
          }}
          onClose={() => setPromoting(null)}
        >
          Jadikan <span className="mono">{promoting.username}</span> leader group {group.name}. Leader menjadi approver migration yang diajukan anggota group ini.
        </ConfirmDialog>
      )}
      {removing && (
        <ConfirmDialog
          title="Keluarkan anggota"
          confirmLabel="Keluarkan"
          danger
          onConfirm={async () => {
            await removeMember(group.id, removing.id)
            await refresh()
            setMessage({ kind: 'success', text: `${removing.username} dikeluarkan dari group ${group.name}.` })
          }}
          onClose={() => setRemoving(null)}
        >
          <span className="mono">{removing.username}</span> tidak lagi mendapat akses database lewat group {group.name}.
        </ConfirmDialog>
      )}
      {deleting && (
        <ConfirmDialog
          title="Hapus group"
          confirmLabel="Hapus group"
          danger
          onConfirm={async () => {
            await deleteGroup(group.id)
            setDeleted(true)
            void navigate('/admin/groups', { replace: true })
            void client.invalidateQueries({ queryKey: ['admin'] })
            void client.invalidateQueries({ queryKey: ['databases'] })
          }}
          onClose={() => setDeleting(false)}
        >
          Group <span className="mono">{group.name}</span> dihapus beserta keanggotaan dan seluruh akses databasenya. User yang menjadi anggotanya tidak ikut terhapus.
        </ConfirmDialog>
      )}
    </>
  )
}
