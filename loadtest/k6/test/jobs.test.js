import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { durationMs, isFinal, queueWaitMs } from '../lib/jobs.js'

describe('status job', () => {
  it('hanya empat status yang final dan sisanya masih dipantau', () => {
    for (const status of ['succeeded', 'failed', 'partial', 'partially_reverted']) assert.equal(isFinal(status), true)
    for (const status of ['queued', 'running', 'uncertain']) assert.equal(isFinal(status), false)
  })
})

describe('waktu job', () => {
  const job = { queued_at: '2026-11-10T10:00:00Z', started_at: '2026-11-10T10:00:02.500Z', finished_at: '2026-11-10T10:00:07Z' }

  it('waktu tunggu antrean adalah selisih mulai dan masuk antrean', () => {
    assert.equal(queueWaitMs(job), 2500)
  })

  it('durasi adalah selisih selesai dan mulai', () => {
    assert.equal(durationMs(job), 4500)
  })

  it('waktu yang belum ada atau tidak valid menghasilkan null', () => {
    assert.equal(queueWaitMs({ queued_at: job.queued_at, started_at: null }), null)
    assert.equal(durationMs({ started_at: job.started_at, finished_at: null }), null)
    assert.equal(durationMs({ started_at: 'ngawur', finished_at: job.finished_at }), null)
  })

  it('selisih tidak pernah negatif walau jam server mundur', () => {
    assert.equal(queueWaitMs({ queued_at: '2026-11-10T10:00:05Z', started_at: '2026-11-10T10:00:00Z' }), 0)
  })
})
