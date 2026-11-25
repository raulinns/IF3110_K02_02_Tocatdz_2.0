import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { submitChange } from '../../api/changes.ts'
import type { Schemas } from '../../api/types.ts'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { requestPath } from '../../lib/paths.ts'

type Recovery = Extract<Schemas['ChangeKind'], 'resume' | 'revert_batches'>

interface RecoveryActionsProps {
  db: string
  job: Schemas['Job']
  migration: Schemas['Migration'] | undefined
}

const copy: Record<Recovery, { partial: string; reverted: string; confirm: string }> = {
  resume: {
    partial: 'Lanjutkan batch yang tersisa sampai migration selesai.',
    reverted: 'Lanjutkan pembatalan batch yang belum sempat dibatalkan.',
    confirm: 'Resume',
  },
  revert_batches: {
    partial: 'Batalkan batch yang sudah di-commit dengan SQL down.',
    reverted: 'Coba membatalkan batch yang masih tersisa dengan SQL down.',
    confirm: 'Revert batch',
  },
}

// Pemulihan tidak mengulang job lama melainkan membuat permintaan baru yang harus disetujui lagi
export function RecoveryActions({ db, job, migration }: RecoveryActionsProps) {
  const navigate = useNavigate()
  const [kind, setKind] = useState<Recovery | null>(null)
  const reverted = job.status === 'partially_reverted'
  const irreversible = migration?.irreversible === true

  const submit = async (chosen: Recovery) => {
    if (!migration) return
    const created = await submitChange(db, { migration_id: migration.id, migration_revision: migration.revision, kind: chosen, source_job_id: job.id })
    void navigate(requestPath(db, created.id), { state: { notice: 'Permintaan pemulihan dibuat dan menunggu approval.' } })
  }

  return (
    <div className="card">
      <div className="card-header">
        <i className="fas fa-life-ring text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0">Pemulihan</h2>
      </div>
      <div className="card-body">
        <Notice kind="warning">
          Job berhenti di tengah dan <strong>database ditahan</strong> sampai dipulihkan. Batch yang sudah di-commit tetap tersimpan. Pilih salah satu cara pemulihan, dan keduanya membuat permintaan baru yang perlu disetujui lagi.
        </Notice>
        <div className="d-flex flex-wrap gap-2">
          <button type="button" className="btn btn-primary" disabled={!migration} onClick={() => setKind('resume')}>
            <i className="fas fa-forward" aria-hidden="true" />
            Resume
          </button>
          <button type="button" className="btn btn-outline-danger" disabled={!migration || irreversible} title={irreversible ? 'Migration irreversible tidak memiliki SQL down' : undefined} onClick={() => setKind('revert_batches')}>
            <i className="fas fa-rotate-left" aria-hidden="true" />
            Revert batches
          </button>
        </div>
      </div>
      {kind && (
        <ConfirmDialog title={kind === 'resume' ? 'Resume job' : 'Revert batch'} confirmLabel={copy[kind].confirm} danger={kind === 'revert_batches'} onConfirm={() => submit(kind)} onClose={() => setKind(null)}>
          {reverted ? copy[kind].reverted : copy[kind].partial} Permintaan baru dibuat dan semua group perlu menyetujuinya sebelum dijalankan.
        </ConfirmDialog>
      )}
    </div>
  )
}
