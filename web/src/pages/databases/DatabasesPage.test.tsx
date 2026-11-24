// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { callsTo, interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const rowOf = async (name: string) => (await screen.findByText(name)).closest('tr') as HTMLElement

describe('daftar database', () => {
  it('menampilkan database yang boleh diakses dengan engine, akses, dan tindakan', async () => {
    openAs('reviewer', '/databases')
    expect(await screen.findByRole('heading', { name: 'Database' })).toBeInTheDocument()
    const inventaris = await rowOf('inventaris')
    expect(within(inventaris).getByText('PostgreSQL')).toBeInTheDocument()
    expect(within(inventaris).getByText('Lihat + ubah')).toBeInTheDocument()
    expect(within(inventaris).getByRole('link', { name: 'Schema' })).toHaveAttribute('href', `/databases/${ids.inventaris}/schema`)
    expect(within(inventaris).getByRole('link', { name: 'Migration' })).toHaveAttribute('href', `/databases/${ids.inventaris}/migrations`)
    const pelaporan = await rowOf('pelaporan')
    expect(within(pelaporan).getByText('Lihat + ubah')).toBeInTheDocument()
    expect(screen.queryByText('arsip')).not.toBeInTheDocument()
  })

  it('daftar terurut menurut nama dan tabel punya keterangan untuk pembaca layar', async () => {
    openAs('observer', '/databases')
    await rowOf('arsip')
    const names = screen.getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('cell')[0].textContent?.trim())
    expect(names).toEqual(['arsip MySQL', 'inventaris PostgreSQL'])
    expect(screen.getByRole('table', { name: 'Database yang dapat Anda akses' })).toBeInTheDocument()
  })

  it('database MySQL tidak menawarkan Migration karena workflow hanya untuk PostgreSQL', async () => {
    openAs('observer', '/databases')
    const arsip = await rowOf('arsip')
    expect(within(arsip).getByText('MySQL')).toBeInTheDocument()
    expect(within(arsip).getByText('Lihat saja')).toBeInTheDocument()
    expect(within(arsip).getByRole('link', { name: 'Schema' })).toBeInTheDocument()
    expect(within(arsip).queryByRole('link', { name: 'Migration' })).not.toBeInTheDocument()
    expect(within(await rowOf('inventaris')).getByRole('link', { name: 'Migration' })).toBeInTheDocument()
  })

  it('platform admin tanpa grant melihat pesan belum punya akses', async () => {
    openAs('admin', '/databases')
    expect(await screen.findByText(/belum memiliki akses ke database mana pun/)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('menampilkan placeholder selama memuat', async () => {
    interceptFetch('/api/v1/databases', { delay: 300 })
    openAs('reviewer', '/databases')
    await screen.findByRole('heading', { name: 'Database' })
    expect(await screen.findByText('Memuat…')).toBeInTheDocument()
    expect(await screen.findByText('inventaris')).toBeInTheDocument()
    expect(screen.queryByText('Memuat…')).not.toBeInTheDocument()
  })

  it('kegagalan jaringan ditampilkan dengan tombol coba lagi yang memuat ulang', async () => {
    const spy = interceptFetch('/api/v1/databases', { failures: 1 })
    const { user } = openAs('reviewer', '/databases')
    expect((await screen.findByRole('alert')).textContent).toContain('Tidak dapat terhubung ke server')
    await user.click(screen.getByRole('button', { name: /Coba lagi/ }))
    expect(await screen.findByText('inventaris')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(callsTo(spy, '/api/v1/databases')).toBe(2)
  })

  it('tombol Schema membuka halaman schema database tersebut', async () => {
    const { user } = openAs('reviewer', '/databases')
    await user.click(within(await rowOf('inventaris')).getByRole('link', { name: 'Schema' }))
    expect(await screen.findByRole('heading', { name: 'public' })).toBeInTheDocument()
  })

  it('daftar yang dipakai layout dan halaman ini hanya diminta sekali', async () => {
    const spy = interceptFetch('/api/v1/databases')
    const { user } = openAs('reviewer', '/databases')
    await user.click(within(await rowOf('inventaris')).getByRole('link', { name: 'Schema' }))
    await screen.findByRole('heading', { name: 'public' })
    expect(callsTo(spy, '/api/v1/databases?')).toBe(1)
  })
})
