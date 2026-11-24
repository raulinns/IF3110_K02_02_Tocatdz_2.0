// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const tabsOf = () => within(screen.getByRole('list', { name: 'Bagian database' })).getAllByRole('link').map((link) => link.textContent?.trim())

describe('layout database', () => {
  it('menampilkan nama, engine, dan akses sebagai pengganti ID', async () => {
    openAs('reviewer', `/databases/${ids.inventaris}/schema`)
    const title = await screen.findByRole('heading', { level: 1 })
    expect(title.textContent).toContain('inventaris')
    expect(title.textContent).toContain('PostgreSQL')
    expect(title.textContent).toContain('Lihat + ubah')
    expect(title.textContent).not.toContain(ids.inventaris)
    const breadcrumb = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(breadcrumb).getByRole('link', { name: 'Database' })).toHaveAttribute('href', '/databases')
    expect(within(breadcrumb).getByText('inventaris')).toHaveAttribute('aria-current', 'page')
  })

  it('database PostgreSQL menampilkan semua tab', async () => {
    openAs('reviewer', `/databases/${ids.inventaris}/schema`)
    await screen.findByRole('list', { name: 'Bagian database' })
    expect(tabsOf()).toEqual(['Schema', 'Migration', 'Schema diff', 'Review', 'Monitor', 'Riwayat'])
  })

  it('database MySQL hanya menampilkan tab Schema', async () => {
    openAs('observer', `/databases/${ids.arsip}/schema`)
    const title = await screen.findByRole('heading', { level: 1 })
    expect(title.textContent).toContain('MySQL')
    expect(title.textContent).toContain('Lihat saja')
    expect(tabsOf()).toEqual(['Schema'])
  })

  it('tab yang aktif ditandai dan setiap tab menuju route yang benar', async () => {
    openAs('reviewer', `/databases/${ids.inventaris}/schema`)
    await screen.findByRole('list', { name: 'Bagian database' })
    expect(screen.getByRole('link', { name: /Schema$/ })).toHaveClass('active')
    expect(screen.getByRole('link', { name: /Riwayat/ })).toHaveAttribute('href', `/databases/${ids.inventaris}/history`)
    expect(screen.getByRole('link', { name: /Schema diff/ })).not.toHaveClass('active')
  })

  it('membuka database tanpa path bagian diarahkan ke schema', async () => {
    openAs('reviewer', `/databases/${ids.inventaris}`)
    expect(await screen.findByRole('heading', { name: 'public' })).toBeInTheDocument()
  })

  it.each([
    ['user tanpa grant', 'auditor', ids.inventaris],
    ['database yang tidak ada', 'reviewer', '10000000-0000-4000-8000-000000000099'],
    ['ID yang bukan UUID', 'reviewer', 'bukan-uuid'],
  ])('%s menampilkan halaman akses ditolak tanpa tab', async (_label, user, db) => {
    openAs(user, `/databases/${db}/schema`)
    expect(await screen.findByText('Akses ditolak')).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Bagian database' })).not.toBeInTheDocument()
  })

  it('platform admin tidak otomatis boleh membuka database', async () => {
    openAs('admin', `/databases/${ids.inventaris}/schema`)
    expect(await screen.findByText('Akses ditolak')).toBeInTheDocument()
  })

  it('kegagalan memuat daftar database dapat dicoba ulang', async () => {
    interceptFetch('/api/v1/databases', { failures: 1 })
    const { user } = openAs('reviewer', `/databases/${ids.inventaris}/schema`)
    expect((await screen.findByRole('alert')).textContent).toContain('Tidak dapat terhubung ke server')
    await user.click(screen.getByRole('button', { name: /Coba lagi/ }))
    expect(await screen.findByRole('heading', { name: 'public' })).toBeInTheDocument()
  })
})
