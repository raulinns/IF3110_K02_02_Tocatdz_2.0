const mutating = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

export function parse(res) {
  try {
    return res.json()
  } catch {
    return null
  }
}

// Klien JSON untuk /api/v1 dengan satu cookie jar per akun, header Origin, dan token CSRF untuk setiap mutasi
export function createClient({ http, baseUrl, origin }) {
  const api = `${baseUrl}/api/v1`

  const newSession = (username, password) => ({ username, password, jar: new http.CookieJar(), csrf: null, user: null })

  function send(session, method, path, { body, name, tags, expected } = {}) {
    const headers = { Accept: 'application/json', Origin: origin }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (mutating.has(method) && session.csrf) headers['X-CSRF-Token'] = session.csrf
    // Nama permintaan tanpa ID supaya jumlah seri metrik tetap kecil
    const params = { headers, jar: session.jar, tags: { name: name || `${method} ${path}`, ...tags } }
    if (expected) params.responseCallback = http.expectedStatuses(...expected)
    const payload = body === undefined ? null : JSON.stringify(body)
    return http.request(method, `${api}${path}`, payload, params)
  }

  function login(session, tags) {
    const res = send(session, 'POST', '/auth/login', { body: { username: session.username, password: session.password }, name: 'POST /auth/login', tags, expected: [200, 401] })
    if (res.status === 200) {
      const me = parse(res)
      session.csrf = me.csrf_token
      session.user = me.user
    }
    return res
  }

  // Sesi yang berakhir atau token CSRF yang basi diperbaiki satu kali lalu permintaan diulang
  function request(session, method, path, options = {}) {
    let res = send(session, method, path, options)
    if (res.status === 401 && login(session, options.tags).status === 200) res = send(session, method, path, options)
    else if (res.status === 403 && mutating.has(method) && parse(res)?.error?.code === 'csrf_failed') {
      const me = send(session, 'GET', '/auth/me', { name: 'GET /auth/me', tags: options.tags })
      if (me.status === 200) {
        session.csrf = parse(me).csrf_token
        res = send(session, method, path, options)
      }
    }
    return res
  }

  // Sesi diekspor dari setup() dan dipakai ulang oleh VU supaya login tidak diulang di tiap VU dan tidak menabrak pembatas login per akun
  const exportSession = (session) => ({ username: session.username, csrf: session.csrf, user: session.user, cookies: session.jar.cookiesForURL(`${api}/`) })

  function adoptSession(saved, password) {
    const session = newSession(saved.username, password)
    session.csrf = saved.csrf
    session.user = saved.user
    for (const [name, values] of Object.entries(saved.cookies)) session.jar.set(`${api}/`, name, Array.isArray(values) ? values[0] : values, { path: '/' })
    return session
  }

  return { newSession, login, request, parse, exportSession, adoptSession }
}
