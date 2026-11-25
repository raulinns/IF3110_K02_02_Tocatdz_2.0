// @vitest-environment jsdom
import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const group = (id: string) => getState().groups.find((candidate) => candidate.id === id)!
const members = async (name: string) => within(await screen.findByRole('table', { name: `Anggota group ${name}` }))
const rowOf = async (name: string, username: string) => (await members(name)).getByText(username).closest('tr') as HTMLElement
const usernames = async (name: string) => (await members(name)).getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('cell')[0].textContent?.replace(/Leader|Nonaktif/g, '').trim())

describe('detail group', () => {
  it('menampilkan anggota dengan penanda leader dan ringkasan di atasnya', async () => {
    openAs('admin', `/admin/groups/${ids.writers}`)
    expect(await usernames('writers')).toEqual(['editor', 'reviewer'])
    const reviewer = await rowOf('writers', 'reviewer')
    expect(within(reviewer).getByText('Leader')).toBeInTheDocument()
    expect(screen.getByText(/2 anggota/)).toBeInTheDocument()
    expect(screen.queryByText(/belum memiliki leader/)).not.toBeInTheDocument()
  })

  it('leader tidak bisa dikeluarkan dan tidak ditawari jadi leader lagi', async () => {
    openAs('admin', `/admin/groups/${ids.writers}`)
    const reviewer = await rowOf('writers', 'reviewer')
    expect(within(reviewer).getByRole('button', { name: 'Keluarkan' })).toBeDisabled()
    expect(within(reviewer).queryByRole('button', { name: 'Jadikan leader' })).not.toBeInTheDocument()
    const editor = await rowOf('writers', 'editor')
    expect(within(editor).getByRole('button', { name: 'Keluarkan' })).toBeEnabled()
    expect(within(editor).getByRole('button', { name: 'Jadikan leader' })).toBeEnabled()
  })

  it('group yang tidak ada menampilkan halaman tidak ditemukan', async () => {
    openAs('admin', '/admin/groups/bukan-group')
    expect(await screen.findByText('Tidak ditemukan')).toBeInTheDocument()
  })

  it('user biasa mendapat halaman akses ditolak', async () => {
    openAs('reviewer', `/admin/groups/${ids.writers}`)
    expect(await screen.findByText(/akses ditolak/i)).toBeInTheDocument()
  })

  it('group tanpa leader dan tanpa anggota menampilkan peringatan dengan petunjuk', async () => {
    openAs('admin', `/admin/groups/${ids.magang}`)
    expect(await screen.findByText(/Tambahkan anggota lebih dulu/)).toBeInTheDocument()
    expect(await screen.findByText('Belum ada anggota.')).toBeInTheDocument()
    expect(screen.getByText(/Leader: belum ada/)).toBeInTheDocument()
  })
})

describe('anggota', () => {
  async function add(user: ReturnType<typeof openAs>['user'], username: string) {
    await screen.findByRole('table', { name: /Anggota group/ })
    await user.clear(screen.getByLabelText('Username'))
    if (username) await user.type(screen.getByLabelText('Username'), username)
    await user.click(screen.getByRole('button', { name: 'Tambahkan' }))
  }

  it('menambahkan anggota dengan username dinormalkan', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.magang}`)
    await add(user, '  Reviewer ')
    expect(await screen.findByRole('status')).toHaveTextContent('reviewer ditambahkan ke group magang.')
    expect(await usernames('magang')).toEqual(['reviewer'])
    expect(group(ids.magang).members).toEqual([ids.reviewer])
    expect(screen.getByLabelText('Username')).toHaveValue('')
    expect(screen.getByLabelText('Username')).toHaveFocus()
  })

  it('username kosong, tidak dikenal, dan yang sudah anggota ditolak tanpa request', async () => {
    const spy = interceptFetch('/members/')
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await add(user, '')
    expect(screen.getByText('Username wajib diisi')).toBeInTheDocument()
    await add(user, 'tidak-ada')
    expect(screen.getByText('User dengan username ini tidak ditemukan')).toBeInTheDocument()
    await add(user, 'editor')
    expect(screen.getByText('User sudah menjadi anggota group ini')).toBeInTheDocument()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'PUT')).toHaveLength(0)
  })

  it('daftar saran hanya berisi user yang belum menjadi anggota', async () => {
    openAs('admin', `/admin/groups/${ids.writers}`)
    await screen.findByRole('table', { name: 'Anggota group writers' })
    // Opsi datalist tidak punya role sehingga dibaca langsung dari DOM setelah daftar user selesai dimuat
    const options = await waitFor(() => {
      const values = Array.from(document.querySelectorAll('datalist option')).map((option) => (option as HTMLOptionElement).value)
      expect(values.length).toBeGreaterThan(0)
      return values
    })
    expect(options).not.toContain('reviewer')
    expect(options).not.toContain('editor')
    expect(options).toContain('observer')
  })

  it('anggota nonaktif diberi penanda dan tidak bisa dijadikan leader', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.magang}`)
    await add(user, 'nonaktif')
    const row = await rowOf('magang', 'nonaktif')
    expect(within(row).getByText('Nonaktif')).toBeInTheDocument()
    expect(within(row).getByRole('button', { name: 'Jadikan leader' })).toBeDisabled()
  })

  it('mengeluarkan anggota setelah konfirmasi', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(within(await rowOf('writers', 'editor')).getByRole('button', { name: 'Keluarkan' }))
    const dialog = screen.getByRole('dialog', { name: 'Keluarkan anggota' })
    expect(dialog).toHaveTextContent('editor')
    await user.click(within(dialog).getByRole('button', { name: 'Keluarkan' }))
    expect(await screen.findByRole('status')).toHaveTextContent('editor dikeluarkan dari group writers.')
    expect(await usernames('writers')).toEqual(['reviewer'])
    expect(group(ids.writers).members).toEqual([ids.reviewer])
  })

  it('membatalkan konfirmasi tidak mengeluarkan siapa pun', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(within(await rowOf('writers', 'editor')).getByRole('button', { name: 'Keluarkan' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(await usernames('writers')).toEqual(['editor', 'reviewer'])
  })

  it('kegagalan saat mengeluarkan tampil di dalam dialog', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(within(await rowOf('writers', 'editor')).getByRole('button', { name: 'Keluarkan' }))
    interceptFetch('/members/', { failures: 1 })
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Keluarkan' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(group(ids.writers).members).toHaveLength(2)
  })
})

describe('leader', () => {
  it('menetapkan leader pada group tanpa leader menghilangkan peringatan', async () => {
    const group = getState().groups.find((candidate) => candidate.id === ids.magang)!
    group.members.push(ids.observer)
    const { user } = openAs('admin', `/admin/groups/${ids.magang}`)
    expect(await screen.findByText(/Pilih salah satu anggota aktif sebagai leader/)).toBeInTheDocument()
    await user.click(within(await rowOf('magang', 'observer')).getByRole('button', { name: 'Jadikan leader' }))
    const dialog = screen.getByRole('dialog', { name: 'Ganti leader' })
    await user.click(within(dialog).getByRole('button', { name: 'Jadikan leader' }))
    expect(await screen.findByRole('status')).toHaveTextContent('observer sekarang menjadi leader group magang.')
    expect(screen.queryByText(/belum memiliki leader/)).not.toBeInTheDocument()
    expect(within(await rowOf('magang', 'observer')).getByText('Leader')).toBeInTheDocument()
    expect(group.leader_user_id).toBe(ids.observer)
  })

  it('mengganti leader memindahkan penanda dan membuka kunci anggota lama', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(within(await rowOf('writers', 'editor')).getByRole('button', { name: 'Jadikan leader' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Jadikan leader' }))
    expect(await screen.findByRole('status')).toHaveTextContent('editor sekarang menjadi leader')
    expect(within(await rowOf('writers', 'editor')).getByText('Leader')).toBeInTheDocument()
    const reviewer = await rowOf('writers', 'reviewer')
    expect(within(reviewer).queryByText('Leader')).not.toBeInTheDocument()
    expect(within(reviewer).getByRole('button', { name: 'Keluarkan' })).toBeEnabled()
    expect(within(await rowOf('writers', 'editor')).getByRole('button', { name: 'Keluarkan' })).toBeDisabled()
  })

  it('membatalkan penggantian leader tidak mengubah apa pun', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(within(await rowOf('writers', 'editor')).getByRole('button', { name: 'Jadikan leader' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(group(ids.writers).leader_user_id).toBe(ids.reviewer)
  })
})

describe('ganti nama dan hapus', () => {
  it('mengganti nama group', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(await screen.findByRole('button', { name: 'Ganti nama' }))
    const dialog = screen.getByRole('dialog', { name: 'Ganti nama group' })
    const input = within(dialog).getByLabelText('Nama group')
    expect(input).toHaveValue('writers')
    expect(input).toHaveFocus()
    await user.clear(input)
    await user.type(input, 'penulis')
    await user.click(within(dialog).getByRole('button', { name: 'Simpan nama' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Nama group diubah menjadi penulis.')
    expect(await screen.findByRole('heading', { name: 'penulis' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(group(ids.writers).name).toBe('penulis')
  })

  it('nama yang sudah dipakai group lain ditampilkan di dalam dialog yang tetap terbuka', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(await screen.findByRole('button', { name: 'Ganti nama' }))
    const dialog = screen.getByRole('dialog')
    await user.clear(within(dialog).getByLabelText('Nama group'))
    await user.type(within(dialog).getByLabelText('Nama group'), 'readers')
    await user.click(within(dialog).getByRole('button', { name: 'Simpan nama' }))
    expect(await within(dialog).findByText('Nama group sudah dipakai')).toBeInTheDocument()
    expect(group(ids.writers).name).toBe('writers')
  })

  it('menyimpan nama yang sama menutup dialog tanpa request', async () => {
    const spy = interceptFetch('/admin/groups/')
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(await screen.findByRole('button', { name: 'Ganti nama' }))
    await user.click(screen.getByRole('button', { name: 'Simpan nama' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'PATCH')).toHaveLength(0)
  })

  it('nama kosong ditolak di dalam dialog', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(await screen.findByRole('button', { name: 'Ganti nama' }))
    await user.clear(screen.getByLabelText('Nama group'))
    await user.click(screen.getByRole('button', { name: 'Simpan nama' }))
    expect(screen.getByText('Nama group wajib diisi')).toBeInTheDocument()
  })

  it('menghapus group beserta akses dan keanggotaannya lalu kembali ke daftar', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(await screen.findByRole('button', { name: 'Hapus group' }))
    const dialog = screen.getByRole('dialog', { name: 'Hapus group' })
    expect(dialog).toHaveTextContent('beserta keanggotaan dan seluruh akses')
    // Perubahan DOM direkam supaya kedipan halaman 404 selama daftar dimuat ulang ikut terdeteksi
    let flashed = false
    const observer = new MutationObserver(() => {
      if (document.body.textContent?.includes('Tidak ditemukan')) flashed = true
    })
    observer.observe(document.body, { childList: true, subtree: true, characterData: true })
    await user.click(within(dialog).getByRole('button', { name: 'Hapus group' }))
    const list = await screen.findByRole('table', { name: 'Daftar group' })
    await waitFor(() => expect(within(list).queryByText('writers')).not.toBeInTheDocument())
    observer.disconnect()
    expect(flashed).toBe(false)
    expect(getState().groups.some((candidate) => candidate.id === ids.writers)).toBe(false)
    expect(getState().grants.some((grant) => grant.group_id === ids.writers)).toBe(false)
  })

  it('membatalkan hapus tidak menghapus group', async () => {
    const { user } = openAs('admin', `/admin/groups/${ids.writers}`)
    await user.click(await screen.findByRole('button', { name: 'Hapus group' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(group(ids.writers)).toBeDefined()
    expect(screen.getByRole('table', { name: 'Anggota group writers' })).toBeInTheDocument()
  })
})
