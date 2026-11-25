import userEvent from '@testing-library/user-event'
import type { InitialEntry } from 'react-router-dom'
import App from '../App.tsx'
import { signInFromUrl } from '../mocks/quick-login.ts'
import { renderApp } from './dom.tsx'

// Membuka aplikasi penuh pada route tertentu sebagai user seed tanpa lewat halaman login
export function openAs(username: string, route: InitialEntry) {
  signInFromUrl(`?as=${username}`)
  return { user: userEvent.setup(), ...renderApp(<App />, route) }
}
