// @vitest-environment jsdom
import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { GuestOnly } from '../auth/guards.tsx'
import { handlers } from '../mocks/handlers/index.ts'
import { demoPassword } from '../mocks/seed.ts'
import { getState } from '../mocks/state.ts'
import { setupMockServer } from '../mocks/testing.ts'
import { renderApp } from '../test/dom.tsx'
import { LoginPage } from './LoginPage.tsx'

setupMockServer(...handlers)

function Landing({ label }: { label: string }) {
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
      <Route path="/databases" element={<Landing label="Daftar database" />} />
      <Route path="/databases/:db/history" element={<Landing label="Riwayat database" />} />
    </Routes>
  )
}

async function openLogin(state?: unknown) {
  renderApp(<Routing />, { pathname: '/login', state })
  await screen.findByRole('heading', { name: 'Masuk ke akun Anda' })
  return userEvent.setup()
}

type User = ReturnType<typeof userEvent.setup>

// Paste jauh lebih cepat daripada mengetik per huruf dan tetap memicu event input seperti pengguna sungguhan
async function fill(user: User, label: string, value: string) {
  const field = screen.getByLabelText(label)
  await user.clear(field)
  if (value) {
    await user.click(field)
    await user.paste(value)
  }
}

async function signIn(user: User, username: string, password: string) {
  await fill(user, 'Username', username)
  await fill(user, 'Password', password)
  await user.click(screen.getByRole('button', { name: 'Masuk' }))
}

const loginCalls = (spy: { mock: { calls: unknown[][] } }) => spy.mock.calls.filter((call) => String(call[0]).includes('/auth/login')).length

describe('tampilan', () => {
  it('menampilkan form, merek, dan footer seperti halaman login M1', async () => {
    await openLogin()
    expect(screen.getByRole('img')).toHaveAttribute('alt', 'IF Tidur, Besok Balik Nubes')
    expect(screen.getByText('Platform pengelolaan database PostgreSQL dan MySQL')).toBeInTheDocument()
    const username = screen.getByLabelText('Username')
    const password = screen.getByLabelText('Password')
    expect(username).toHaveFocus()
    expect(username).toHaveAttribute('autocomplete', 'username')
    expect(username).toHaveAttribute('autocapitalize', 'none')
    expect(password).toHaveAttribute('type', 'password')
    expect(password).toHaveAttribute('autocomplete', 'current-password')
    expect(screen.getByText('Akun dibuat oleh admin platform.')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('tombol mata menampilkan dan menyembunyikan password', async () => {
    const user = await openLogin()
    const password = screen.getByLabelText('Password')
    await user.click(screen.getByRole('button', { name: 'Tampilkan password' }))
    expect(password).toHaveAttribute('type', 'text')
    await user.click(screen.getByRole('button', { name: 'Sembunyikan password' }))
    expect(password).toHaveAttribute('type', 'password')
  })

  it('tombol tema di footer mengganti tema dan menyimpannya', async () => {
    const user = await openLogin()
    const toggle = screen.getByRole('button', { name: 'Ganti tema gelap atau terang' })
    expect(toggle).toHaveAttribute('aria-pressed', 'false')
    await user.click(toggle)
    expect(document.documentElement).toHaveAttribute('data-bs-theme', 'dark')
    expect(window.localStorage.getItem('tocatdz-theme')).toBe('dark')
    expect(toggle).toHaveAttribute('aria-pressed', 'true')
    await user.click(toggle)
    expect(document.documentElement).toHaveAttribute('data-bs-theme', 'light')
  })
})

describe('validasi di sisi client', () => {
  it('form kosong ditandai per field tanpa mengirim request login', async () => {
    const spy = vi.spyOn(globalThis, 'fetch')
    const user = await openLogin()
    await user.click(screen.getByRole('button', { name: 'Masuk' }))
    expect(screen.getByText('Username wajib diisi')).toBeInTheDocument()
    expect(screen.getByText('Password wajib diisi')).toBeInTheDocument()
    expect(screen.getByLabelText('Username')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('Username')).toHaveFocus()
    expect(loginCalls(spy)).toBe(0)
  })

  it('field yang diperbaiki langsung kehilangan pesan errornya', async () => {
    const user = await openLogin()
    await user.click(screen.getByRole('button', { name: 'Masuk' }))
    await user.type(screen.getByLabelText('Username'), 'r')
    expect(screen.queryByText('Username wajib diisi')).not.toBeInTheDocument()
    expect(screen.getByText('Password wajib diisi')).toBeInTheDocument()
  })

  it('hanya password yang kosong memindahkan fokus ke password', async () => {
    const user = await openLogin()
    await signIn(user, 'reviewer', '')
    expect(screen.getByLabelText('Password')).toHaveFocus()
    expect(screen.queryByText('Username wajib diisi')).not.toBeInTheDocument()
  })
})

describe('login gagal', () => {
  it('pesan generik yang sama untuk password salah dan username yang tidak ada', async () => {
    const user = await openLogin()
    await signIn(user, 'reviewer', 'salah-sekali')
    const wrongPassword = (await screen.findByRole('alert')).textContent
    expect(wrongPassword).toBe('Username atau password salah.')
    await signIn(user, 'tidak-terdaftar', 'salah-sekali')
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe(wrongPassword))
    expect(screen.queryByText(/tidak ditemukan|tidak terdaftar|nonaktif/i)).not.toBeInTheDocument()
  })

  it('username dipertahankan, password dikosongkan, dan fokus pindah ke password', async () => {
    const user = await openLogin()
    await signIn(user, 'reviewer', 'salah-sekali')
    await screen.findByRole('alert')
    expect(screen.getByLabelText('Username')).toHaveValue('reviewer')
    expect(screen.getByLabelText('Password')).toHaveValue('')
    expect(screen.getByLabelText('Password')).toHaveFocus()
    expect(screen.getByRole('button', { name: 'Masuk' })).toBeEnabled()
  })

  it('pesan error hilang saat percobaan berikutnya berhasil dikirim ulang', async () => {
    const user = await openLogin()
    await signIn(user, 'reviewer', 'salah-sekali')
    await screen.findByRole('alert')
    await fill(user, 'Password', demoPassword)
    await user.click(screen.getByRole('button', { name: 'Masuk' }))
    expect(await screen.findByText('Daftar database')).toBeInTheDocument()
  })

  it('validasi dari server ditampilkan di bawah field-nya, bukan sebagai alert', async () => {
    const user = await openLogin()
    await signIn(user, 'a'.repeat(200), 'apa-saja')
    expect(await screen.findByText('Username terlalu panjang')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Username')).toHaveAttribute('aria-invalid', 'true')
  })

  it('gangguan jaringan memberi pesan yang jelas dan form tetap bisa dipakai', async () => {
    const user = await openLogin()
    const original = globalThis.fetch
    vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => (String(input).includes('/auth/login') ? Promise.reject(new TypeError('Failed to fetch')) : original(input, init)))
    await signIn(user, 'reviewer', demoPassword)
    expect((await screen.findByRole('alert')).textContent).toContain('Tidak dapat terhubung ke server')
    expect(screen.getByLabelText('Password')).toHaveValue(demoPassword)
    expect(screen.getByRole('button', { name: 'Masuk' })).toBeEnabled()
  })
})

describe('login berhasil', () => {
  it('kembali ke halaman asal lengkap dengan query-nya', async () => {
    const user = await openLogin({ from: '/databases/db1/history?hide_rollback=true' })
    await signIn(user, 'reviewer', demoPassword)
    expect(await screen.findByText('Riwayat database?hide_rollback=true')).toBeInTheDocument()
  })

  it('tanpa halaman asal masuk ke daftar database dan username dinormalkan oleh server', async () => {
    const user = await openLogin()
    await signIn(user, '  Reviewer ', demoPassword)
    expect(await screen.findByText('Daftar database')).toBeInTheDocument()
  })

  it.each(['//evil.example/phish', 'https://evil.example', '/login', '/\\evil.example'])('halaman asal yang tidak aman %s diabaikan', async (from) => {
    const user = await openLogin({ from })
    await signIn(user, 'reviewer', demoPassword)
    expect(await screen.findByText('Daftar database')).toBeInTheDocument()
  })

  it('selama request berjalan tombol menampilkan Memproses dan klik berulang tidak mengirim ganda', async () => {
    const original = globalThis.fetch
    const spy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      if (String(input).includes('/auth/login')) await new Promise((resolve) => setTimeout(resolve, 200))
      return original(input, init)
    })
    const user = await openLogin()
    await fill(user, 'Username', 'reviewer')
    await fill(user, 'Password', demoPassword)
    await user.dblClick(screen.getByRole('button', { name: 'Masuk' }))
    expect(await screen.findByRole('button', { name: /Memproses/ })).toBeDisabled()
    await user.keyboard('{Enter}')
    await screen.findByText('Daftar database')
    expect(loginCalls(spy)).toBe(1)
  })

  it('dua submit beruntun tanpa jeda render tetap hanya mengirim satu request', async () => {
    const original = globalThis.fetch
    const spy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      if (String(input).includes('/auth/login')) await new Promise((resolve) => setTimeout(resolve, 100))
      return original(input, init)
    })
    const user = await openLogin()
    await fill(user, 'Username', 'reviewer')
    await fill(user, 'Password', demoPassword)
    const form = screen.getByRole('button', { name: 'Masuk' }).closest('form') as HTMLFormElement
    fireEvent.submit(form)
    fireEvent.submit(form)
    await screen.findByText('Daftar database')
    expect(loginCalls(spy)).toBe(1)
  })

  it('admin juga masuk dan sesi tersimpan untuk halaman berikutnya', async () => {
    const user = await openLogin()
    await signIn(user, 'admin', demoPassword)
    await screen.findByText('Daftar database')
    expect(getState().session?.userId).toBeTruthy()
  })
})

describe('pembatasan percobaan', () => {
  it('menampilkan pesan, mengunci tombol dengan hitung mundur, lalu membukanya kembali', { timeout: 10_000 }, async () => {
    getState().lockedUntil = Date.now() + 1200
    const user = await openLogin()
    await signIn(user, 'reviewer', demoPassword)
    expect((await screen.findByRole('alert')).textContent).toContain('Terlalu banyak percobaan masuk')
    const button = screen.getByRole('button', { name: /Coba lagi dalam \d+ detik/ })
    expect(button).toBeDisabled()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Masuk' })).toBeEnabled(), { timeout: 4000 })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Masuk' }))
    expect(await screen.findByText('Daftar database')).toBeInTheDocument()
  })

  it('form tidak mengirim request selama terkunci walau di-submit lewat Enter', async () => {
    getState().lockedUntil = Date.now() + 3000
    const spy = vi.spyOn(globalThis, 'fetch')
    const user = await openLogin()
    await signIn(user, 'reviewer', demoPassword)
    await screen.findByRole('alert')
    const before = loginCalls(spy)
    await user.type(screen.getByLabelText('Password'), '{Enter}')
    expect(loginCalls(spy)).toBe(before)
  })

  it('lima kegagalan beruntun mengunci percobaan berikutnya', { timeout: 10_000 }, async () => {
    const user = await openLogin()
    for (let attempt = 0; attempt < 5; attempt++) {
      await signIn(user, 'reviewer', 'salah-sekali')
      await waitFor(() => expect(screen.getByRole('button', { name: 'Masuk' })).toBeEnabled())
    }
    await signIn(user, 'reviewer', demoPassword)
    expect(await screen.findByRole('button', { name: /Coba lagi dalam \d+ detik/ })).toBeDisabled()
  })
})
