import { parse } from './client.js'
import { isFinal } from './jobs.js'
import { approve, execute } from './workflow.js'

const openStatuses = ['pending_review', 'approved', 'queued', 'running']

// Judul migration buatan k6 berawalan ini, dan hanya permintaan seperti itu yang boleh diselesaikan otomatis
export const ownedTitlePrefix = 'k6 '

const list = (deps, plan, path, name) => deps.client.request(plan.editor, 'GET', `/databases/${plan.database.id}${path}`, { name, tags: { stage: plan.stage() } })

// Menunggu job selesai tanpa mencatat metrik beban karena ini bukan bagian dari pengukuran
function waitForJob(deps, plan, jobId) {
  const deadline = deps.now() + deps.config.jobTimeoutSeconds * 1000
  for (;;) {
    const res = deps.client.request(plan.editor, 'GET', `/jobs/${jobId}`, { name: 'GET /jobs/{id}', tags: { stage: plan.stage() } })
    if (res.status === 200 && isFinal(parse(res).status)) return true
    if (deps.now() >= deadline) return false
    deps.sleep(deps.config.pollSeconds)
  }
}

// Permintaan terbuka sisa run sebelumnya menahan database karena hanya satu permintaan boleh terbuka, sehingga milik k6 diselesaikan dulu
// dan yang bukan milik k6 menghentikan setup dengan keterangan, tanpa disentuh
export function drainOpenRequests(deps, plan) {
  const requests = list(deps, plan, '/change-requests?page=1&limit=200', 'GET /databases/{id}/change-requests')
  if (requests.status !== 200) return { error: `daftar permintaan ${plan.database.name} gagal dengan status ${requests.status}` }
  const open = parse(requests).items.filter((request) => openStatuses.includes(request.status))
  if (open.length === 0) return { drained: [] }
  const migrations = list(deps, plan, '/migrations?page=1&limit=200', 'GET /databases/{id}/migrations')
  if (migrations.status !== 200) return { error: `daftar migration ${plan.database.name} gagal dengan status ${migrations.status}` }
  const titles = new Map(parse(migrations).items.map((migration) => [migration.id, migration.title]))
  const drained = []
  for (const request of open) {
    if (!(titles.get(request.migration_id) ?? '').startsWith(ownedTitlePrefix)) {
      return { error: `database ${plan.database.name} ditahan permintaan ${request.id} (${request.status}) yang bukan milik k6, selesaikan atau tolak dulu` }
    }
    let current = request
    if (current.status === 'pending_review') {
      const approved = approve(deps, plan, current)
      if (approved.error) return { error: `permintaan sisa ${request.id} gagal disetujui (${approved.error})` }
      current = approved.request
    }
    if (current.status === 'approved') {
      const accepted = execute(deps, plan, current)
      if (accepted.error) return { error: `permintaan sisa ${request.id} gagal dijalankan (${accepted.error})` }
      if (!waitForJob(deps, plan, accepted.jobId)) return { error: `job permintaan sisa ${request.id} tidak selesai dalam batas waktu` }
    } else if (!waitForRequest(deps, plan, request.id)) {
      return { error: `permintaan sisa ${request.id} tidak selesai dalam batas waktu` }
    }
    drained.push(request.id)
  }
  return { drained }
}

// Permintaan yang sudah berjalan menunggu hasil akhir lewat statusnya
function waitForRequest(deps, plan, requestId) {
  const deadline = deps.now() + deps.config.jobTimeoutSeconds * 1000
  for (;;) {
    const res = deps.client.request(plan.editor, 'GET', `/databases/${plan.database.id}/change-requests/${requestId}`, { name: 'GET /databases/{id}/change-requests/{id}', tags: { stage: plan.stage() } })
    if (res.status === 200 && !openStatuses.includes(parse(res).status)) return true
    if (deps.now() >= deadline) return false
    deps.sleep(deps.config.pollSeconds)
  }
}
