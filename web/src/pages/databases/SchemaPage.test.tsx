// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const schemaRoute = `/databases/${ids.inventaris}/schema`
const cardOf = (name: string) => screen.getByRole('heading', { name }).closest('.card') as HTMLElement

describe('halaman schema', () => {
  it('mengelompokkan table per schema dengan jumlah dan jenisnya', async () => {
    openAs('reviewer', schemaRoute)
    await screen.findByRole('heading', { name: 'public' })
    expect(screen.getAllByRole('heading', { level: 2 }).map((heading) => heading.textContent)).toEqual(['audit', 'public'])
    expect(within(cardOf('audit')).getByText('1 table')).toBeInTheDocument()
    expect(within(cardOf('public')).getByText('4 table')).toBeInTheDocument()
    const publicRows = within(cardOf('public')).getAllByRole('row').slice(1)
    expect(publicRows.map((row) => row.textContent?.trim())).toEqual(['customerstable', 'orderstable', 'productstable', 'v_stok_menipisview'])
  })

  it('setiap table menaut ke halaman detailnya', async () => {
    openAs('reviewer', schemaRoute)
    await screen.findByRole('heading', { name: 'public' })
    expect(screen.getByRole('link', { name: 'orders' })).toHaveAttribute('href', `/databases/${ids.inventaris}/schema/public/orders`)
    expect(screen.getByRole('link', { name: 'log_entries' })).toHaveAttribute('href', `/databases/${ids.inventaris}/schema/audit/log_entries`)
  })

  it('view_only juga boleh membaca schema', async () => {
    openAs('observer', schemaRoute)
    expect(await screen.findByRole('heading', { name: 'public' })).toBeInTheDocument()
  })

  it('schema database MySQL memakai nama database sebagai schema', async () => {
    openAs('observer', `/databases/${ids.arsip}/schema`)
    await screen.findByRole('heading', { name: 'arsip' })
    expect(screen.getByRole('link', { name: 'dokumen' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'kategori' })).toBeInTheDocument()
  })

  it('kotak pencarian menyaring table di semua schema dan menyembunyikan schema yang kosong', async () => {
    const { user } = openAs('reviewer', schemaRoute)
    await screen.findByRole('heading', { name: 'public' })
    const search = screen.getByRole('searchbox', { name: 'Cari table' })
    await user.type(search, 'ORD')
    expect(screen.getByRole('link', { name: 'orders' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'products' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'audit' })).not.toBeInTheDocument()
    expect(within(cardOf('public')).getByText('1 table')).toBeInTheDocument()
    await user.clear(search)
    expect(screen.getByRole('link', { name: 'products' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'audit' })).toBeInTheDocument()
  })

  it('pencarian tanpa hasil menampilkan pesan dengan kata yang dicari', async () => {
    const { user } = openAs('reviewer', schemaRoute)
    await screen.findByRole('heading', { name: 'public' })
    await user.type(screen.getByRole('searchbox', { name: 'Cari table' }), 'tidak-ada')
    expect(screen.getByText(/Tidak ada table yang cocok dengan/)).toHaveTextContent('tidak-ada')
    expect(screen.queryByRole('heading', { level: 2 })).not.toBeInTheDocument()
  })

  it('database tanpa table menampilkan pesan kosong tanpa kotak pencarian', async () => {
    const id = '10000000-0000-4000-8000-0000000000aa'
    getState().connections.push({ id, name: 'kosong', engine: 'postgresql' })
    getState().grants.push({ group_id: ids.writers, database_id: id, access: 'view_only' })
    openAs('reviewer', `/databases/${id}/schema`)
    expect(await screen.findByText('Database ini belum memiliki schema yang dapat ditampilkan.')).toBeInTheDocument()
    expect(screen.queryByRole('searchbox')).not.toBeInTheDocument()
  })

  it('kegagalan memuat schema dapat dicoba ulang', async () => {
    interceptFetch('/schema', { failures: 1 })
    const { user } = openAs('reviewer', schemaRoute)
    expect((await screen.findByRole('alert')).textContent).toContain('Tidak dapat terhubung ke server')
    await user.click(screen.getByRole('button', { name: /Coba lagi/ }))
    expect(await screen.findByRole('heading', { name: 'public' })).toBeInTheDocument()
  })
})
