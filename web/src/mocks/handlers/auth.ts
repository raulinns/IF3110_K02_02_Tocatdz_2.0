import { http } from 'msw/http'
import { ApiFailure, noContent, ok, open, readJson, requireNoBody, route, secured, stringField, unauthenticated, utf8Length } from '../http.ts'
import { authMe } from '../models.ts'
import { getState, loginFailureLimit, loginLockMs, newCsrfToken, setSession } from '../state.ts'
import { Fields, normalizeUsername } from '../validation.ts'

// Login yang gagal berulang dikunci sementara dan Retry-After menghitung mundur sisa waktunya
function throttled(remainingMs: number) {
  return new ApiFailure(429, 'rate_limited', 'Terlalu banyak percobaan', { headers: { 'Retry-After': String(Math.ceil(remainingMs / 1000)) } })
}

export const authHandlers = [
  http.post(
    route('/auth/login'),
    open(async ({ request }) => {
      const state = getState()
      const now = Date.now()
      if (state.lockedUntil > now) throw throttled(state.lockedUntil - now)
      const body = await readJson(request, ['username', 'password'])
      const rawUsername = stringField(body, 'username') ?? ''
      const password = stringField(body, 'password') ?? ''
      const fields = new Fields()
      fields.add('username', rawUsername === '' ? 'Username wajib diisi' : utf8Length(rawUsername) > 128 ? 'Username terlalu panjang' : null)
      fields.add('password', password === '' ? 'Password wajib diisi' : utf8Length(password) > 1024 ? 'Password terlalu panjang' : null)
      fields.check()
      const username = normalizeUsername(rawUsername)
      const user = state.users.find((candidate) => candidate.username === username && candidate.active)
      if (!user || user.password !== password) {
        state.loginFailures += 1
        if (state.loginFailures >= loginFailureLimit) {
          state.loginFailures = 0
          state.lockedUntil = now + loginLockMs
        }
        throw unauthenticated('Username atau password tidak valid')
      }
      state.loginFailures = 0
      const session = { userId: user.id, csrfToken: newCsrfToken() }
      setSession(session)
      return ok(authMe(user, session))
    }),
  ),

  http.get(
    route('/auth/me'),
    secured(({ user, session }) => ok(authMe(user, session))),
  ),

  http.post(
    route('/auth/logout'),
    secured(async ({ request }) => {
      await requireNoBody(request)
      setSession(null)
      return noContent()
    }),
  ),
]
