// Daftar endpoint baca untuk satu database, dengan nama tanpa ID agar metrik per endpoint tetap sedikit
// Endpoint workflow bisa dikecualikan untuk server yang belum memasangnya, karena endpoint itu akan menjawab 404
export function readTargets(database, { workflow = true } = {}) {
  const base = `/databases/${database.id}`
  const targets = [
    { name: 'GET /databases', path: '/databases?page=1&limit=50' },
    { name: 'GET /databases/{id}/schema', path: `${base}/schema` },
    { name: 'GET /databases/{id}/position', path: `${base}/position`, workflow: true },
    { name: 'GET /databases/{id}/executions', path: `${base}/executions?page=1&limit=50`, workflow: true },
    { name: 'GET /databases/{id}/executions hide_rollback', path: `${base}/executions?hide_rollback=true&page=1&limit=50`, workflow: true },
    { name: 'GET /databases/{id}/migrations', path: `${base}/migrations?page=1&limit=50`, workflow: true },
    { name: 'GET /databases/{id}/change-requests', path: `${base}/change-requests?page=1&limit=50`, workflow: true },
    { name: 'GET /databases/{id}/jobs', path: `${base}/jobs?page=1&limit=50`, workflow: true },
  ]
  if (database.table) {
    const { schema, name } = database.table
    targets.push({ name: 'GET /databases/{id}/tables/{schema}/{table}/rows', path: `${base}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(name)}/rows?page=1&limit=50` })
  }
  return workflow ? targets : targets.filter((target) => !target.workflow)
}

export const pickTarget = (database, random, options) => {
  const targets = readTargets(database, options)
  return targets[Math.min(targets.length - 1, Math.floor(random() * targets.length))]
}

// Satu pembacaan diberi tag jenis dan tahap, dan hanya status 200 yang dianggap sehat
export function readOnce({ client, session, database, stage, random, check, workflow = true }) {
  const target = pickTarget(database, random, { workflow })
  const res = client.request(session, 'GET', target.path, { name: target.name, tags: { kind: 'read', stage: stage() } })
  check(res, { [`${target.name} 200`]: (response) => response.status === 200 })
  return res
}
