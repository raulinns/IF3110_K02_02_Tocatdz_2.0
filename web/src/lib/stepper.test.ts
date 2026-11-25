import { describe, expect, it } from 'vitest'
import type { Schemas } from '../api/types.ts'
import { jobSteps } from './stepper.ts'

const states = (status: Schemas['JobStatus']) => jobSteps(status).map((step) => step.state)

describe('jobSteps', () => {
  it('selalu enam tahap dengan tiga tahap awal sudah lewat', () => {
    for (const status of ['queued', 'running', 'succeeded', 'failed', 'partial', 'uncertain', 'partially_reverted'] as const) {
      const steps = jobSteps(status)
      expect(steps).toHaveLength(6)
      expect(steps.slice(0, 3).map((step) => step.state)).toEqual(['done', 'done', 'done'])
    }
  })

  it('job antre berada di tahap Antre dan sisanya belum tiba', () => {
    expect(states('queued')).toEqual(['done', 'done', 'done', 'current', 'upcoming', 'upcoming'])
  })

  it('job berjalan sudah melewati antrean', () => {
    expect(states('running')).toEqual(['done', 'done', 'done', 'done', 'current', 'upcoming'])
  })

  it('hasil akhir diberi nama dan warna menurut hasilnya', () => {
    expect(jobSteps('succeeded').at(-1)).toEqual({ label: 'Berhasil', state: 'success' })
    expect(jobSteps('failed').at(-1)).toEqual({ label: 'Gagal', state: 'danger' })
    expect(jobSteps('partial').at(-1)).toEqual({ label: 'Sebagian', state: 'warning' })
    expect(jobSteps('uncertain').at(-1)).toEqual({ label: 'Belum pasti', state: 'warning' })
    expect(jobSteps('partially_reverted').at(-1)).toEqual({ label: 'Sebagian dibatalkan', state: 'warning' })
  })
})
