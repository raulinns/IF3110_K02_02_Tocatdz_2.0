import { afterAll, afterEach, beforeAll } from 'vitest'
import type { AnyHandler } from 'msw'
import { setupServer } from 'msw/node'
import { ApiClient } from '../api/client.ts'
import { ApiError } from '../api/errors.ts'
import type { Schemas } from '../api/types.ts'
import { demoPassword } from './seed.ts'
import { resetState } from './state.ts'

// Menyalakan server MSW untuk satu file test dan mengembalikan data ke kondisi awal setelah tiap test
export function setupMockServer(...handlers: AnyHandler[]) {
  const server = setupServer(...handlers)
  beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))
  afterEach(() => resetState())
  afterAll(() => server.close())
}

// Node tidak menerima URL relatif sehingga base ditambahkan di sini
export function newClient() {
  return new ApiClient((input, init) => fetch(new URL(String(input), 'http://localhost'), init))
}

// Menunggu request yang diharapkan gagal dan mengembalikan ApiError-nya
export async function failureOf(work: Promise<unknown>): Promise<ApiError> {
  try {
    await work
  } catch (err) {
    if (err instanceof ApiError) return err
    throw err
  }
  throw new Error('request seharusnya gagal')
}

export async function signIn(username: string, password = demoPassword) {
  const client = newClient()
  const me = await client.post<Schemas['AuthMe']>('/auth/login', { username, password }, { handleUnauthenticated: false })
  client.configure({ csrfToken: me.csrf_token })
  return { client, me }
}
