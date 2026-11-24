import userEvent from '@testing-library/user-event'
import App from '../App.tsx'
import { signInFromUrl } from '../mocks/quick-login.ts'
import { renderApp } from './dom.tsx'

// Membuka aplikasi penuh pada route tertentu sebagai user seed tanpa lewat halaman login
export function openAs(username: string, route: string) {
  signInFromUrl(`?as=${username}`)
  return { user: userEvent.setup(), ...renderApp(<App />, route) }
}
