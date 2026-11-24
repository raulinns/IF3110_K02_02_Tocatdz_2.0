// @vitest-environment jsdom
import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'

setupMockServer(...handlers)

const tableRoute = (schema: string, table: string, db: string = ids.inventaris) => `/databases/${db}/schema/${encodeURIComponent(schema)}/${encodeURIComponent(table)}`
const cardOf = (name: string) => screen.getByRole('heading', { name }).closest('.card') as HTMLElement
const bodyRows = (card: HTMLElement) => within(card).getAllByRole('row').slice(1)

describe('detail tabel', () => {
  it('menampilkan kolom beserta tipe, nullable, dan default', async () => {
    openAs('reviewer', tableRoute('public', 'orders'))
    await screen.findByRole('heading', { name: 'Kolom' })
    const card = cardOf('Kolom')
    expect(within(card).getByText('7')).toBeInTheDocument()
    const rows = bodyRows(card).map((row) => within(row).getAllByRole('cell').map((cell) => cell.textContent))
    expect(rows[0]).toEqual(['id', 'bigint', 'Tidak', '-'])
    expect(rows[4]).toEqual(['total', 'numeric(12,2)', 'Tidak', '-'])
    expect(rows[5]).toEqual(['status', 'text', 'Tidak', "'baru'::text"])
    expect(rows).toHaveLength(7)
  })

  it('menampilkan constraint dengan jenis dan definisinya', async () => {
    openAs('reviewer', tableRoute('public', 'orders'))
    await screen.findByRole('heading', { name: 'Constraint' })
    const rows = bodyRows(cardOf('Constraint')).map((row) => within(row).getAllByRole('cell').map((cell) => cell.textContent))
    expect(rows.map((row) => row[1])).toEqual(['PRIMARY KEY', 'FOREIGN KEY', 'FOREIGN KEY', 'CHECK'])
    expect(rows[1]).toEqual(['orders_customer_id_fkey', 'FOREIGN KEY', 'FOREIGN KEY (customer_id) REFERENCES public.customers(id)'])
    expect(within(cardOf('Constraint')).getByText('4')).toBeInTheDocument()
  })

  it('menampilkan index dan menandai primary key', async () => {
    openAs('reviewer', tableRoute('public', 'orders'))
    await screen.findByRole('heading', { name: 'Index' })
    const rows = bodyRows(cardOf('Index')).map((row) => within(row).getAllByRole('cell').map((cell) => cell.textContent))
    expect(rows[0].slice(0, 2)).toEqual(['orders_pkey', 'Ya (primary key)'])
    expect(rows[1].slice(0, 2)).toEqual(['idx_orders_customer', 'Tidak'])
  })

  it('judul, breadcrumb, jenis, dan tombol Lihat data', async () => {
    openAs('reviewer', tableRoute('public', 'orders'))
    const heading = await screen.findByRole('heading', { name: /public\.orders/ })
    await waitFor(() => expect(heading).toHaveTextContent('table'))
    const pageBreadcrumb = screen.getAllByRole('navigation', { name: 'Breadcrumb' })[1]
    expect(within(pageBreadcrumb).getByRole('link', { name: 'Schema' })).toHaveAttribute('href', `/databases/${ids.inventaris}/schema`)
    expect(within(pageBreadcrumb).getByText('public.orders')).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByRole('link', { name: 'Lihat data' })).toHaveAttribute('href', `/databases/${ids.inventaris}/schema/public/orders/data`)
  })

  it('view tidak punya constraint dan index sehingga muncul keterangan kosong', async () => {
    openAs('reviewer', tableRoute('public', 'v_stok_menipis'))
    await screen.findByRole('heading', { name: 'Kolom' })
    expect(screen.getByRole('heading', { name: /public\.v_stok_menipis/ })).toHaveTextContent('view')
    expect(screen.getByText('Tidak ada constraint.')).toBeInTheDocument()
    expect(screen.getByText('Tidak ada index.')).toBeInTheDocument()
  })

  it('table yang tidak ada menampilkan halaman tidak ditemukan', async () => {
    openAs('reviewer', tableRoute('public', 'tidak_ada'))
    expect(await screen.findByText('Tidak ditemukan')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Lihat data' })).not.toBeInTheDocument()
  })

  it('tabel database MySQL dapat dibuka oleh user view_only', async () => {
    openAs('observer', tableRoute('arsip', 'dokumen', ids.arsip))
    await screen.findByRole('heading', { name: 'Kolom' })
    expect(within(cardOf('Kolom')).getByText('varchar(200)')).toBeInTheDocument()
    expect(within(cardOf('Constraint')).getByText('fk_dokumen_kategori')).toBeInTheDocument()
  })

  it('nama dengan spasi, garis miring, dan tanda tanya tetap terbuka lewat link dari halaman schema', async () => {
    const products = getState().tables.find((table) => table.name === 'products')!
    getState().tables.push({ ...products, schema: 'sch ema', name: 'tabel/unik?1' })
    const { user } = openAs('reviewer', `/databases/${ids.inventaris}/schema`)
    await user.click(await screen.findByRole('link', { name: 'tabel/unik?1' }))
    expect(await screen.findByRole('heading', { name: /sch ema\.tabel\/unik\?1/ })).toBeInTheDocument()
    expect(within(cardOf('Kolom')).getByText('sku')).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Lihat data' }))
    expect(await screen.findByRole('heading', { name: /Data/ })).toHaveTextContent('sch ema.tabel/unik?1')
    expect(await screen.findByRole('columnheader', { name: 'sku' })).toBeInTheDocument()
  })

  it('breadcrumb Schema kembali ke daftar table', async () => {
    const { user } = openAs('reviewer', tableRoute('public', 'orders'))
    await screen.findByRole('heading', { name: 'Kolom' })
    await user.click(within(screen.getAllByRole('navigation', { name: 'Breadcrumb' })[1]).getByRole('link', { name: 'Schema' }))
    expect(await screen.findByRole('heading', { name: 'audit' })).toBeInTheDocument()
  })
})
