// @vitest-environment jsdom
import { act, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { api } from '../api/client.ts'
import { handlers } from '../mocks/handlers/index.ts'
import { signInFromUrl } from '../mocks/quick-login.ts'
import { demoPassword } from '../mocks/seed.ts'
import { setSession } from '../mocks/state.ts'
import { setupMockServer } from '../mocks/testing.ts'
import { LoginPage } from '../pages/LoginPage.tsx'
import { renderApp } from '../test/dom.tsx'
import { GuestOnly, RequireAdmin, RequireAuth } from './guards.tsx'

setupMockServer(...handlers)

function Page({ label }: { label: string }) {
  const location = useLocation()
  return (
    <p>
      {label}
      {location.search}
    </p>
  )
}

function Routing() {
  return (
    <Routes>
      <Route element={<GuestOnly />}>
        <Route path="/login" element={<LoginPage />} />
      </Route>
      <Route element={<RequireAuth />}>
        <Route path="/databases" element={<Page label="Daftar database" />} />
        <Route path="/databases/:db/history" element={<Page label="Riwayat database" />} />
        <Route element={<RequireAdmin />}>
          <Route path="/admin/users" element={<Page label="Halaman admin" />} />
        </Route>
      </Route>
    </Routes>
  )
}

const loginHeading = () => screen.findByRole('heading', { name: 'Masuk ke akun Anda' })

describe('RequireAuth dan GuestOnly', () => {
  it('halaman terlindungi mengalihkan ke login lalu kembali ke tujuan semula setelah masuk', async () => {
    renderApp(<Routing />, '/databases/db1/history?hide_rollback=true')
    await loginHeading()
    const user = userEvent.setup()
    await user.click(screen.getByLabelText('Username'))
    await user.paste('reviewer')
    await user.click(screen.getByLabelText('Password'))
    await user.paste(demoPassword)
    await user.click(screen.getByRole('button', { name: 'Masuk' }))
    expect(await screen.findByText('Riwayat database?hide_rollback=true')).toBeInTheDocument()
  })

  it('pengguna yang sudah masuk tidak melihat halaman login lagi', async () => {
    signInFromUrl('?as=reviewer')
    renderApp(<Routing />, '/login')
    expect(await screen.findByText('Daftar database')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Masuk ke akun Anda' })).not.toBeInTheDocument()
  })

  it('pengguna yang sudah masuk membuka login dari halaman lain kembali ke halaman tersebut', async () => {
    signInFromUrl('?as=reviewer')
    renderApp(<Routing />, { pathname: '/login', state: { from: '/databases/db1/history' } })
    expect(await screen.findByText('Riwayat database')).toBeInTheDocument()
  })

  it('tanpa sesi semua route terlindungi menuju login', async () => {
    renderApp(<Routing />, '/admin/users')
    await loginHeading()
    expect(screen.queryByText('Halaman admin')).not.toBeInTheDocument()
  })
})

describe('RequireAdmin', () => {
  it('non-admin melihat halaman akses ditolak', async () => {
    signInFromUrl('?as=reviewer')
    renderApp(<Routing />, '/admin/users')
    expect(await screen.findByText(/akses ditolak/i)).toBeInTheDocument()
    expect(screen.queryByText('Halaman admin')).not.toBeInTheDocument()
  })

  it('admin platform masuk ke halaman admin', async () => {
    signInFromUrl('?as=admin')
    renderApp(<Routing />, '/admin/users')
    expect(await screen.findByText('Halaman admin')).toBeInTheDocument()
  })
})

describe('sesi berakhir saat aplikasi dipakai', () => {
  it('respons 401 membuang sesi dan mengalihkan ke login', async () => {
    signInFromUrl('?as=reviewer')
    renderApp(<Routing />, '/databases')
    expect(await screen.findByText('Daftar database')).toBeInTheDocument()
    setSession(null)
    await act(async () => {
      await expect(api.get('/databases')).rejects.toMatchObject({ status: 401 })
    })
    await loginHeading()
  })
})
