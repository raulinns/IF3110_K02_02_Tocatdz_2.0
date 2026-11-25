import type { Schemas } from '../api/types.ts'
import { runStatusLabel } from './labels.ts'

export type StepState = 'done' | 'current' | 'upcoming' | 'success' | 'danger' | 'warning'

export interface Step {
  label: string
  state: StepState
}

const lead = ['Draft', 'Menunggu review', 'Disetujui']

// Tiga tahap awal pasti sudah lewat karena job hanya ada setelah permintaan disetujui, lalu antre, berjalan, dan hasil akhir
export function jobSteps(status: Schemas['JobStatus']): Step[] {
  const steps: Step[] = lead.map((label) => ({ label, state: 'done' }))
  steps.push({ label: 'Antre', state: status === 'queued' ? 'current' : 'done' })
  steps.push({ label: 'Berjalan', state: status === 'queued' ? 'upcoming' : status === 'running' ? 'current' : 'done' })
  if (status === 'queued' || status === 'running') {
    steps.push({ label: 'Hasil', state: 'upcoming' })
  } else {
    const state: StepState = status === 'succeeded' ? 'success' : status === 'failed' ? 'danger' : 'warning'
    steps.push({ label: runStatusLabel(status), state })
  }
  return steps
}
