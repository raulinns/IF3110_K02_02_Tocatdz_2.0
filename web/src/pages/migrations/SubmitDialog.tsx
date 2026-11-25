import type { Schemas } from '../../api/types.ts'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { submitApply } from '../../api/changes.ts'

interface SubmitDialogProps {
  db: string
  migration: Schemas['Migration']
  onSubmitted: () => Promise<void>
  onClose: () => void
}

export function SubmitDialog({ db, migration, onSubmitted, onClose }: SubmitDialogProps) {
  return (
    <ConfirmDialog
      title="Ajukan untuk review"
      confirmLabel="Ajukan"
      onConfirm={async () => {
        await submitApply(db, migration)
        await onSubmitted()
      }}
      onClose={onClose}
    >
      <p>
        Migration <strong>{migration.title}</strong> (revision <span className="mono">{migration.revision}</span>) diajukan untuk direview oleh leader setiap group yang memiliki akses ke database ini.
      </p>
      <p className="mb-0">Setelah diajukan, SQL dan konfigurasinya dibekukan dan tidak bisa diubah lagi. Jika ditolak, salin ke draft baru untuk memperbaikinya.</p>
    </ConfirmDialog>
  )
}
