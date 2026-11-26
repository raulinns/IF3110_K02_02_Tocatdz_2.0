// Daftar endpoint baca untuk satu database, dengan nama tanpa ID agar metrik per endpoint tetap sedikit
export function readTargets(database) {
  const base = `/databases/${database.id}`
  const targets = [
    { name: 'GET /databases', path: '/databases?page=1&limit=50' },
    { name: 'GET /databases/{id}/schema', path: `${base}/schema` },
    { name: 'GET /databases/{id}/position', path: `${base}/position` },
    { name: 'GET /databases/{id}/executions', path: `${base}/executions?page=1&limit=50` },
    { name: 'GET /databases/{id}/executions hide_rollback', path: `${base}/executions?hide_rollback=true&page=1&limit=50` },
    { name: 'GET /databases/{id}/migrations', path: `${base}/migrations?page=1&limit=50` },
    { name: 'GET /databases/{id}/change-requests', path: `${base}/change-requests?page=1&limit=50` },
    { name: 'GET /databases/{id}/jobs', path: `${base}/jobs?page=1&limit=50` },
  ]
  if (database.table) {
    const { schema, name } = database.table
    targets.push({ name: 'GET /databases/{id}/tables/{schema}/{table}/rows', path: `${base}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(name)}/rows?page=1&limit=50` })
  }
  return targets
}

export const pickTarget = (database, random) => {
  const targets = readTargets(database)
  return targets[Math.min(targets.length - 1, Math.floor(random() * targets.length))]
}

// Satu pembacaan diberi tag jenis dan tahap, dan hanya status 200 yang dianggap sehat
export function readOnce({ client, session, database, stage, random, check }) {
  const target = pickTarget(database, random)
  const res = client.request(session, 'GET', target.path, { name: target.name, tags: { kind: 'read', stage: stage() } })
  check(res, { [`${target.name} 200`]: (response) => response.status === 200 })
  return res
}
