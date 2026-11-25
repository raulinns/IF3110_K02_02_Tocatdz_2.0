import { runStatusLabel, runStatusTone, type RunStatus, type Tone } from '../../lib/labels.ts'

const toneClass: Record<Tone, string> = { success: ' status-applied', danger: ' status-failed', pending: ' status-pending', warning: ' status-pending' }

// Status permintaan dan job ditampilkan dengan label dan warna yang sama di semua halaman workflow
export function StatusBadge({ status }: { status: RunStatus }) {
  return (
    <span className={`badge status${toneClass[runStatusTone(status)]}`}>
      {status === 'running' && <i className="fas fa-spinner fa-spin me-1" aria-hidden="true" />}
      {runStatusLabel(status)}
    </span>
  )
}
