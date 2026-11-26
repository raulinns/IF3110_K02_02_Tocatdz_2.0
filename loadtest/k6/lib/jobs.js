const finalStatuses = ['succeeded', 'failed', 'partial', 'partially_reverted']

// Job queued, running, dan uncertain masih berjalan, dan 202 saat mengantre tidak pernah dihitung sebagai hasil
export const isFinal = (status) => finalStatuses.includes(status)

const time = (value) => {
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? null : parsed
}

// Waktu tunggu antrean adalah selisih mulai dan masuk antrean menurut jam server
export function queueWaitMs(job) {
  const queued = time(job.queued_at)
  const started = time(job.started_at)
  return queued === null || started === null ? null : Math.max(0, started - queued)
}

export function durationMs(job) {
  const started = time(job.started_at)
  const finished = time(job.finished_at)
  return started === null || finished === null ? null : Math.max(0, finished - started)
}
