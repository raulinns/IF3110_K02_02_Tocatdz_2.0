import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { submitChange } from '../../api/changes.ts'
import { migrationQuery } from '../../api/migrations.ts'
import type { Schemas } from '../../api/types.ts'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { jobPath, requestPath } from '../../lib/paths.ts'

type Kind = Extract<Schemas['ChangeKind'], 'rollback' | 'forward'>

interface PositionCardProps {
  db: string
  position: Schemas['Position']
  title: (migrationId: string | null) => string | undefined
  canEdit: boolean
}

// Rollback dan forward dibuat sebagai permintaan baru yang menunjuk eksekusi tujuannya dan tetap perlu disetujui
export function PositionCard({ db, position, title, canEdit }: PositionCardProps) {
  const navigate = useNavigate()
  const [kind, setKind] = useState<Kind | null>(null)
  const rollbackTarget = position.active_migration_id
  const forwardTarget = position.forward_migration_id
  const rollbackMigration = useQuery({ ...migrationQuery(db, rollbackTarget ?? ''), enabled: rollbackTarget !== null && canEdit })
  const forwardMigration = useQuery({ ...migrationQuery(db, forwardTarget ?? ''), enabled: forwardTarget !== null && canEdit })
  const irreversible = rollbackMigration.data?.irreversible === true

  const submit = async (chosen: Kind) => {
    const migration = chosen === 'rollback' ? rollbackMigration.data : forwardMigration.data
    const target = chosen === 'rollback' ? position.rollback_execution_id : position.forward_execution_id
    if (!migration || !target) return
    const created = await submitChange(db, { migration_id: migration.id, migration_revision: migration.revision, kind: chosen, target_execution_id: target })
    void navigate(requestPath(db, created.id), { state: { notice: `Permintaan ${chosen} dibuat dan menunggu approval.` } })
  }

  return (
    <div className="card">
      <div className="card-header">
        <i className="fas fa-location-dot text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0">Posisi database</h2>
        <span className="card-header-meta">versi {position.version}</span>
      </div>
      <div className="card-body">
        {position.blocked_by_job_id && (
          <Notice kind="warning">
            Database sedang ditahan oleh <Link to={jobPath(db, position.blocked_by_job_id)}>sebuah job</Link> yang belum selesai atau belum dipulihkan, sehingga rollback dan forward belum tersedia.
          </Notice>
        )}
        <dl className="facts mb-3">
          <dt>Migration aktif</dt>
          <dd>{position.active_migration_id ? <strong>{title(position.active_migration_id) ?? position.active_migration_id.slice(0, 8)}</strong> : <span className="text-muted">Belum ada migration yang dijalankan</span>}</dd>
          <dt>Rollback</dt>
          <dd>{position.can_rollback ? 'Tersedia untuk migration aktif' : irreversible ? 'Tidak tersedia: migration aktif ditandai irreversible' : 'Tidak tersedia'}</dd>
          <dt>Forward</dt>
          <dd>{position.can_forward ? <>Tersedia: {title(position.forward_migration_id) ?? position.forward_migration_id?.slice(0, 8)}</> : 'Tidak ada migration yang menunggu untuk dijalankan kembali'}</dd>
        </dl>
        {canEdit ? (
          <div className="d-flex flex-wrap gap-2">
            <button type="button" className="btn btn-outline-danger" disabled={!position.can_rollback || !rollbackMigration.data} title={!position.can_rollback ? 'Rollback tidak tersedia' : undefined} onClick={() => setKind('rollback')}>
              <i className="fas fa-rotate-left" aria-hidden="true" />
              Rollback
            </button>
            <button type="button" className="btn btn-outline-primary" disabled={!position.can_forward || !forwardMigration.data} title={!position.can_forward ? 'Tidak ada forward yang tersedia' : undefined} onClick={() => setKind('forward')}>
              <i className="fas fa-rotate-right" aria-hidden="true" />
              Forward
            </button>
          </div>
        ) : (
          <p className="mb-0 small text-muted">Rollback dan forward hanya bisa diajukan oleh pengguna dengan akses ubah pada database ini.</p>
        )}
      </div>
      {kind && (
        <ConfirmDialog title={kind === 'rollback' ? 'Ajukan rollback' : 'Ajukan forward'} confirmLabel={kind === 'rollback' ? 'Ajukan rollback' : 'Ajukan forward'} danger={kind === 'rollback'} onConfirm={() => submit(kind)} onClose={() => setKind(null)}>
          {kind === 'rollback' ? (
            <>
              SQL down dari <strong>{title(position.active_migration_id) ?? 'migration aktif'}</strong> akan dijalankan untuk membatalkannya.
            </>
          ) : (
            <>
              SQL up dari <strong>{title(position.forward_migration_id) ?? 'migration berikutnya'}</strong> akan dijalankan kembali.
            </>
          )}{' '}
          Permintaan ini perlu disetujui semua group sebelum dijalankan.
        </ConfirmDialog>
      )}
    </div>
  )
}
