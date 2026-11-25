import type { Schemas } from '../api/types.ts'

export const accessLabel = (access: Schemas['Access']) => (access === 'view_edit' ? 'Lihat + ubah' : 'Lihat saja')

export const engineLabel = (engine: Schemas['Database']['engine']) => (engine === 'mysql' ? 'MySQL' : 'PostgreSQL')

export const tableKindLabel = (kind: Schemas['TableKind']) => kind.replace(/_/g, ' ')

export const modeLabel = (mode: Schemas['ExecutionMode']) => (mode === 'batch' ? 'Batch' : 'Normal')

export const definitionStateLabel = (state: Schemas['DefinitionState']) => (state === 'frozen' ? 'Dibekukan' : 'Draft')

export const changeKindLabel = (kind: Schemas['ChangeKind']) =>
  ({ apply: 'Terapkan', rollback: 'Rollback', forward: 'Forward', resume: 'Resume', revert_batches: 'Revert batch' })[kind]

export type RunStatus = Schemas['CRStatus'] | Schemas['JobStatus']

const runStatusLabels: Record<RunStatus, string> = {
  pending_review: 'Menunggu review',
  approved: 'Disetujui',
  rejected: 'Ditolak',
  queued: 'Antre',
  running: 'Berjalan',
  succeeded: 'Berhasil',
  failed: 'Gagal',
  partial: 'Sebagian',
  uncertain: 'Belum pasti',
  partially_reverted: 'Sebagian dibatalkan',
}

export const runStatusLabel = (status: RunStatus) => runStatusLabels[status]

export type Tone = 'success' | 'danger' | 'pending' | 'warning'

// Warna badge mengikuti hasilnya: hijau berhasil, merah gagal, kuning masih berjalan atau perlu tindakan
export const runStatusTone = (status: RunStatus): Tone =>
  status === 'succeeded' || status === 'approved' ? 'success' : status === 'failed' || status === 'rejected' ? 'danger' : status === 'partial' || status === 'uncertain' || status === 'partially_reverted' ? 'warning' : 'pending'

export const decisionLabel = (decision: Schemas['Decision'] | null) => (decision === 'approve' ? 'Disetujui' : decision === 'reject' ? 'Ditolak' : 'Menunggu')

