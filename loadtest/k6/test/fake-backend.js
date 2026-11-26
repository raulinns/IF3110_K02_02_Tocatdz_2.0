const epoch = 1_800_000_000_000

// Backend palsu yang meniru kontrak /api/v1 secara sinkron seperti http k6, dengan jam buatan supaya job berjalan menurut waktu tidur skrip
export function createBackend(options = {}) {
  const settings = { runMs: 3000, queueMs: 500, failJobs: false, stuckJobs: false, conflicts: 0, loginRateLimits: 0, expireFirstSession: false, passwords: {}, ...options }
  const clock = { t: 0 }
  const iso = (offset) => new Date(epoch + offset).toISOString()
  const users = {
    editor: { id: 'u-editor', username: 'editor' },
    reviewer: { id: 'u-reviewer', username: 'reviewer' },
    observer: { id: 'u-observer', username: 'observer' },
  }
  const databases = [
    { id: 'db-1', name: 'inventaris', engine: 'postgresql', access: 'view_edit', capabilities: { workflow: true } },
    { id: 'db-2', name: 'pelaporan', engine: 'postgresql', access: 'view_edit', capabilities: { workflow: true } },
    { id: 'db-3', name: 'arsip', engine: 'mysql', access: 'view_only', capabilities: { workflow: false } },
    { id: 'db-4', name: 'catatan', engine: 'postgresql', access: 'view_only', capabilities: { workflow: true } },
  ]
  const state = { sessions: {}, migrations: [], requests: [], jobs: [], log: [], logins: 0, conflictsLeft: settings.conflicts, rateLimitsLeft: settings.loginRateLimits, expired: false, nextId: 1 }
  const id = (prefix) => `${prefix}-${state.nextId++}`

  class CookieJar {
    constructor() {
      this.cookies = {}
    }

    // Mengikuti bentuk jar k6: cookiesForURL mengembalikan nama dengan daftar nilai, dan set menerima url, nama, nilai, opsi
    cookiesForURL() {
      return Object.fromEntries(Object.entries(this.cookies).map(([name, value]) => [name, [value]]))
    }

    set(url, name, value) {
      this.cookies[name] = value
    }
  }

  const respond = (status, body, headers = {}) => ({ status, headers, body: body === undefined ? '' : JSON.stringify(body), json() { return JSON.parse(this.body) } })
  const failure = (status, code, reason) => respond(status, { error: { code, message: code, request_id: 'r', reason } })

  const jobView = (job) => {
    const age = clock.t - job.queuedAtOffset
    if (settings.stuckJobs) return { ...job, status: age >= settings.queueMs ? 'running' : 'queued', queued_at: iso(job.queuedAtOffset), started_at: age >= settings.queueMs ? iso(job.queuedAtOffset + settings.queueMs) : null, finished_at: null }
    if (age < settings.queueMs) return { ...job, status: 'queued', queued_at: iso(job.queuedAtOffset), started_at: null, finished_at: null }
    const startedOffset = job.queuedAtOffset + settings.queueMs
    if (age < settings.queueMs + settings.runMs) return { ...job, status: 'running', queued_at: iso(job.queuedAtOffset), started_at: iso(startedOffset), finished_at: null }
    return { ...job, status: settings.failJobs ? 'failed' : 'succeeded', queued_at: iso(job.queuedAtOffset), started_at: iso(startedOffset), finished_at: iso(startedOffset + settings.runMs) }
  }

  // Satu database hanya menerima satu permintaan terbuka sampai job-nya selesai, sama seperti aturan alur M2
  const openRequest = (databaseId) =>
    state.requests.find((request) => {
      if (request.database_id !== databaseId) return false
      if (request.status === 'queued') return !['succeeded', 'failed'].includes(jobView(state.jobs.find((job) => job.change_request_id === request.id)).status)
      return request.status === 'pending_review' || request.status === 'approved'
    })

  function handle(method, rawPath, body, params) {
    const headers = params.headers ?? {}
    const path = rawPath.replace(/^.*\/api\/v1/, '').split('?')[0]
    const record = { method, path, tags: params.tags, expected: params.responseCallback?.codes, headers, sid: params.jar?.cookies?.sid }
    state.log.push(record)
    if (headers.Origin !== 'http://localhost:8080') return failure(403, 'forbidden', 'origin')
    if (path === '/auth/login') {
      if (state.rateLimitsLeft > 0) {
        state.rateLimitsLeft -= 1
        return respond(429, { error: { code: 'rate_limited', message: 'x', request_id: 'r' } }, { 'Retry-After': '2' })
      }
      const input = JSON.parse(body)
      const user = users[input.username]
      if (!user || input.password !== (settings.passwords[input.username] ?? 'pw')) return failure(401, 'unauthenticated')
      state.logins += 1
      const sid = id('sid')
      state.sessions[sid] = { user, csrf: `csrf-${sid}`, fresh: settings.expireFirstSession && !state.expired }
      params.jar.cookies.sid = sid
      return respond(200, { user, platform_admin: false, csrf_token: state.sessions[sid].csrf, leader_of: [] })
    }
    const session = state.sessions[params.jar.cookies.sid]
    if (!session) return failure(401, 'unauthenticated')
    if (session.fresh) {
      session.fresh = false
      state.expired = true
      delete state.sessions[params.jar.cookies.sid]
      return failure(401, 'unauthenticated')
    }
    if (method !== 'GET') {
      if (headers['X-CSRF-Token'] !== session.csrf) return failure(403, 'csrf_failed')
      if (body !== null && headers['Content-Type'] !== 'application/json') return failure(415, 'unsupported_media_type')
    }
    if (path === '/auth/me') return respond(200, { user: session.user, platform_admin: false, csrf_token: session.csrf, leader_of: [] })
    if (path === '/databases') return respond(200, { items: databases, page: 1, limit: 200, has_next: false })
    let match = /^\/databases\/([^/]+)\/schema$/.exec(path)
    if (match) return respond(200, { schemas: [{ name: 'public', tables: [{ name: 'orders', kind: 'table' }] }] })
    // Status permintaan yang sudah berjalan mengikuti hasil job-nya seperti pada server sungguhan
    const statusOf = (request) => {
      if (request.status !== 'queued') return request
      const job = state.jobs.find((candidate) => candidate.change_request_id === request.id)
      return { ...request, status: job && ['succeeded', 'failed'].includes(jobView(job).status) ? 'succeeded' : 'queued' }
    }
    match = /^\/databases\/([^/]+)\/change-requests$/.exec(path)
    if (match && method === 'GET') return respond(200, { items: state.requests.filter((request) => request.database_id === match[1]).map(statusOf), page: 1, limit: 200, has_next: false })
    match = /^\/databases\/([^/]+)\/change-requests\/([^/]+)$/.exec(path)
    if (match && method === 'GET') {
      const request = state.requests.find((candidate) => candidate.id === match[2])
      return request ? respond(200, statusOf(request)) : failure(404, 'not_found')
    }
    match = /^\/databases\/([^/]+)\/migrations$/.exec(path)
    if (match && method === 'GET') return respond(200, { items: state.migrations.filter((migration) => migration.database_id === match[1]), page: 1, limit: 200, has_next: false })
    match = /^\/databases\/([^/]+)\/(position|executions|migrations|change-requests|jobs|tables\/[^/]+\/[^/]+\/rows)$/.exec(path)
    if (match && method === 'GET') return respond(200, { items: [], page: 1, limit: 50, has_next: false })
    match = /^\/databases\/([^/]+)\/migrations$/.exec(path)
    if (match && method === 'POST') {
      const input = JSON.parse(body)
      if (!input.title || !input.up_sql || !input.down_sql || input.mode !== 'normal' || input.batch !== null) return failure(422, 'validation_failed')
      const migration = { id: id('mig'), database_id: match[1], revision: 1, ...input }
      state.migrations.push(migration)
      return respond(201, migration)
    }
    match = /^\/databases\/([^/]+)\/change-requests$/.exec(path)
    if (match && method === 'POST') {
      const input = JSON.parse(body)
      if (state.conflictsLeft > 0) {
        state.conflictsLeft -= 1
        return failure(409, 'conflict')
      }
      if (openRequest(match[1])) return failure(409, 'conflict')
      const request = {
        id: id('cr'),
        database_id: match[1],
        migration_id: input.migration_id,
        revision: 1,
        kind: input.kind,
        status: 'pending_review',
        approvals: [
          { group_id: 'g-writers', leader_user_id: users.reviewer.id, decision: null },
          { group_id: 'g-readers', leader_user_id: users.observer.id, decision: null },
        ],
      }
      state.requests.push(request)
      return respond(201, request)
    }
    match = /^\/databases\/([^/]+)\/change-requests\/([^/]+)\/approvals$/.exec(path)
    if (match) {
      const input = JSON.parse(body)
      const request = state.requests.find((candidate) => candidate.id === match[2])
      const approval = request?.approvals.find((candidate) => candidate.group_id === input.group_id)
      if (!approval) return failure(422, 'validation_failed')
      if (approval.leader_user_id !== session.user.id) return failure(403, 'forbidden')
      if (input.revision !== request.revision) return failure(409, 'conflict', 'revision_mismatch')
      approval.decision = input.decision
      request.revision += 1
      if (request.approvals.every((candidate) => candidate.decision === 'approve')) request.status = 'approved'
      return respond(200, request)
    }
    match = /^\/databases\/([^/]+)\/change-requests\/([^/]+)\/execute$/.exec(path)
    if (match) {
      const request = state.requests.find((candidate) => candidate.id === match[2])
      if (!request || request.status !== 'approved' || JSON.parse(body).revision !== request.revision) return failure(409, 'conflict', 'revision_mismatch')
      const job = { id: id('job'), database_id: match[1], change_request_id: request.id, queuedAtOffset: clock.t }
      state.jobs.push(job)
      request.status = 'queued'
      return respond(202, { job_id: job.id, status: 'queued' })
    }
    match = /^\/jobs\/([^/]+)$/.exec(path)
    if (match) {
      const job = state.jobs.find((candidate) => candidate.id === match[1])
      return job ? respond(200, jobView(job)) : failure(404, 'not_found')
    }
    return failure(404, 'not_found')
  }

  const http = {
    CookieJar,
    expectedStatuses: (...codes) => ({ codes }),
    request: (method, url, body, params) => handle(method, url, body, params),
  }
  return { http, clock, state, users, databases, now: () => epoch + clock.t, sleep: (seconds) => (clock.t += seconds * 1000) }
}

// Metrik palsu yang hanya mencatat nilai dan tag yang ditambahkan
export function createMetrics() {
  const make = () => {
    const values = []
    return { values, add: (value, tags) => values.push({ value, tags }) }
  }
  return { jobsCompleted: make(), jobsFailed: make(), jobsTimedOut: make(), submitConflicts: make(), rateLimited: make(), flowErrors: make(), jobSuccess: make(), queueWait: make(), jobDuration: make(), jobEndToEnd: make(), submitWait: make() }
}
