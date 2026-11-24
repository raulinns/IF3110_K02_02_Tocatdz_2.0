// @vitest-environment jsdom
import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { callsTo, interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const rowsRoute = (schema: string, table: string, query = '', db: string = ids.inventaris) => `/databases/${db}/schema/${schema}/${table}/data${query}`
const body = () => within(screen.getByRole('table')).getAllByRole('row').slice(1)
const firstCells = () => body().map((row) => within(row).getAllByRole('cell')[0].textContent)
const info = () => screen.getByRole('navigation', { name: 'Halaman data' }).textContent

describe('data baris', () => {
  it('halaman pertama memuat 50 baris dengan nama kolom sebagai header', async () => {
    openAs('reviewer', rowsRoute('public', 'orders'))
    await screen.findByRole('columnheader', { name: 'customer_id' })
    expect(screen.getAllByRole('columnheader').map((header) => header.textContent)).toEqual(['id', 'customer_id', 'product_id', 'jumlah', 'total', 'status', 'dibuat'])
    expect(body()).toHaveLength(50)
    expect(firstCells()[0]).toBe('1')
    expect(info()).toContain('Halaman 1 · 50 baris per halaman')
    expect(screen.getByRole('heading', { name: /Data/ })).toHaveTextContent('public.orders')
  })

  it('tabel lebar berada di dalam area scroll yang bisa difokus keyboard', async () => {
    openAs('reviewer', rowsRoute('public', 'orders'))
    const region = await screen.findByRole('region', { name: 'Isi tabel public.orders' })
    expect(region).toHaveClass('table-responsive')
    expect(region).toHaveAttribute('tabindex', '0')
    expect(within(region).getByRole('table', { name: 'Isi tabel public.orders halaman 1' })).toHaveClass('table-data')
    expect(within(region).queryByRole('caption')).not.toBeInTheDocument()
  })

  it('tombol Berikutnya membuka halaman 2 dan Sebelumnya kembali', async () => {
    const { user } = openAs('reviewer', rowsRoute('public', 'orders'))
    await screen.findByRole('columnheader', { name: 'id' })
    expect(screen.getByRole('button', { name: /Sebelumnya/ })).toBeDisabled()
    await user.click(screen.getByRole('link', { name: /Berikutnya/ }))
    await waitFor(() => expect(info()).toContain('Halaman 2'))
    expect(firstCells()[0]).toBe('51')
    await user.click(screen.getByRole('link', { name: /Sebelumnya/ }))
    await waitFor(() => expect(info()).toContain('Halaman 1'))
    expect(firstCells()[0]).toBe('1')
  })

  it('halaman terakhir memuat sisa baris dan tidak punya halaman berikutnya', async () => {
    openAs('reviewer', rowsRoute('public', 'orders', '?page=3'))
    await waitFor(() => expect(body()).toHaveLength(37))
    expect(firstCells().at(-1)).toBe('137')
    expect(screen.getByRole('button', { name: /Berikutnya/ })).toBeDisabled()
    expect(screen.getByRole('link', { name: /Sebelumnya/ })).toBeEnabled()
  })

  it('mengganti jumlah baris per halaman kembali ke halaman pertama', async () => {
    const { user } = openAs('reviewer', rowsRoute('public', 'orders', '?page=2'))
    await screen.findByRole('columnheader', { name: 'id' })
    await user.selectOptions(screen.getByLabelText('Baris per halaman'), '100')
    await waitFor(() => expect(info()).toContain('Halaman 1 · 100 baris per halaman'))
    await waitFor(() => expect(body()).toHaveLength(100))
    expect(firstCells()[0]).toBe('1')
  })

  it('tautan langsung dengan page dan limit dihormati, dan nilai yang tidak valid memakai bawaan', async () => {
    openAs('reviewer', rowsRoute('public', 'orders', '?page=2&limit=100'))
    await waitFor(() => expect(info()).toContain('Halaman 2 · 100 baris per halaman'))
    expect(firstCells()[0]).toBe('101')
  })

  it('jumlah baris yang dipilih terbawa saat pindah halaman', async () => {
    const { user } = openAs('reviewer', rowsRoute('public', 'orders', '?limit=100'))
    await waitFor(() => expect(body()).toHaveLength(100))
    expect(screen.getByRole('link', { name: /Berikutnya/ })).toHaveAttribute('href', expect.stringContaining('limit=100'))
    await user.click(screen.getByRole('link', { name: /Berikutnya/ }))
    await waitFor(() => expect(info()).toContain('Halaman 2 · 100 baris per halaman'))
    expect(screen.getByLabelText('Baris per halaman')).toHaveValue('100')
    await waitFor(() => expect(firstCells()[0]).toBe('101'))
  })

  it.each(['?page=abc&limit=7','?page=0&limit=-5', '?page=1.5&limit=999'])('query %s kembali ke bawaan', async (query) => {
    openAs('reviewer', rowsRoute('public', 'orders', query))
    await waitFor(() => expect(info()).toContain('Halaman 1 · 50 baris per halaman'))
    expect(body()).toHaveLength(50)
  })

  it('halaman di luar jangkauan menawarkan kembali ke halaman pertama', async () => {
    const { user } = openAs('reviewer', rowsRoute('public', 'orders', '?page=9'))
    expect(await screen.findByText('Tidak ada baris di halaman ini.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Berikutnya/ })).toBeDisabled()
    await user.click(screen.getByRole('link', { name: 'Kembali ke halaman pertama' }))
    await waitFor(() => expect(body()).toHaveLength(50))
  })

  it('tabel tanpa baris menampilkan keterangan kosong', async () => {
    const products = getState().tables.find((table) => table.name === 'products')!
    getState().tables.push({ ...products, name: 'kosong', rows: [] })
    openAs('reviewer', rowsRoute('public', 'kosong'))
    expect(await screen.findByText('Tabel ini belum memiliki baris.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Kembali ke halaman pertama' })).not.toBeInTheDocument()
  })

  it('NULL, hex, dan teks yang dipotong ditandai, dan tabel tanpa urutan unik diberi peringatan', async () => {
    openAs('reviewer', rowsRoute('audit', 'log_entries'))
    expect(await screen.findByRole('note')).toHaveTextContent('tidak punya urutan unik')
    const rows = body()
    expect(within(rows[3]).getByText('NULL')).toHaveClass('null')
    expect(within(rows[0]).getByText('hex')).toHaveClass('badge')
    expect(within(rows[0]).getByText(/\\x00000001/)).toBeInTheDocument()
    expect(within(rows[2]).getByText('dipotong')).toHaveClass('badge')
  })

  it('tabel dengan urutan primary key tidak menampilkan peringatan', async () => {
    openAs('reviewer', rowsRoute('public', 'customers'))
    await screen.findByRole('columnheader', { name: 'email' })
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
    const email = screen.getAllByRole('columnheader').findIndex((header) => header.textContent === 'email')
    expect(within(body()[4]).getAllByRole('cell')[email]).toHaveTextContent('NULL')
    expect(within(body()[0]).getAllByRole('cell')[email]).toHaveTextContent('pelanggan1@contoh.id')
  })

  it('view dan tabel MySQL juga dapat dilihat isinya', async () => {
    const view = openAs('reviewer', rowsRoute('public', 'v_stok_menipis'))
    await screen.findByRole('columnheader', { name: 'sku' })
    expect(body().length).toBeGreaterThan(0)
    view.unmount()
    openAs('observer', rowsRoute('arsip', 'dokumen', '?limit=25', ids.arsip))
    await waitFor(() => expect(body()).toHaveLength(25))
  })

  it('tombol Muat ulang mengambil data lagi', async () => {
    const spy = interceptFetch('/rows')
    const { user } = openAs('reviewer', rowsRoute('public', 'orders'))
    await screen.findByRole('columnheader', { name: 'id' })
    expect(callsTo(spy, '/rows')).toBe(1)
    await user.click(screen.getByRole('button', { name: 'Muat ulang' }))
    await waitFor(() => expect(callsTo(spy, '/rows')).toBe(2))
  })

  it('kegagalan memuat data dapat dicoba ulang tanpa kehilangan kontrol halaman', async () => {
    interceptFetch('/rows', { failures: 1 })
    const { user } = openAs('reviewer', rowsRoute('public', 'orders'))
    expect((await screen.findByRole('alert')).textContent).toContain('Tidak dapat terhubung ke server')
    expect(screen.getByLabelText('Baris per halaman')).toBeEnabled()
    await user.click(screen.getByRole('button', { name: /Coba lagi/ }))
    await screen.findByRole('columnheader', { name: 'id' })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('tabel yang tidak ada menampilkan halaman tidak ditemukan', async () => {
    openAs('reviewer', rowsRoute('public', 'tidak_ada'))
    expect(await screen.findByText('Tidak ditemukan')).toBeInTheDocument()
    expect(screen.getByLabelText('Baris per halaman')).toBeInTheDocument()
  })

  it('breadcrumb menuju daftar table dan detail tabel', async () => {
    openAs('reviewer', rowsRoute('public', 'orders'))
    await screen.findByRole('columnheader', { name: 'id' })
    const breadcrumb = screen.getAllByRole('navigation', { name: 'Breadcrumb' })[1]
    expect(within(breadcrumb).getByRole('link', { name: 'Schema' })).toHaveAttribute('href', `/databases/${ids.inventaris}/schema`)
    expect(within(breadcrumb).getByRole('link', { name: 'public.orders' })).toHaveAttribute('href', `/databases/${ids.inventaris}/schema/public/orders`)
    expect(within(breadcrumb).getByText('Data')).toHaveAttribute('aria-current', 'page')
  })
})
