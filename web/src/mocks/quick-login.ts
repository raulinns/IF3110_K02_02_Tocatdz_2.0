import { getState, newCsrfToken, setSession } from './state.ts'

// Membuka halaman dengan ?as=username langsung masuk sebagai user seed tanpa lewat halaman login
export function signInFromUrl(search: string): boolean {
  const username = new URLSearchParams(search).get('as')
  const user = getState().users.find((candidate) => candidate.username === username && candidate.active)
  if (!user) return false
  setSession({ userId: user.id, csrfToken: newCsrfToken() })
  return true
}
