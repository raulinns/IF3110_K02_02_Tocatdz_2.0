import { afterEach, describe, expect, it } from 'vitest'
import { ids } from './seed.ts'
import { accessFor, getState, newCsrfToken, resetState, setSession } from './state.ts'

afterEach(() => resetState())

describe('accessFor', () => {
  it('mengikuti grant group tempat user menjadi anggota', () => {
    expect(accessFor(ids.reviewer, ids.inventaris)).toBe('view_edit')
    expect(accessFor(ids.observer, ids.inventaris)).toBe('view_only')
    expect(accessFor(ids.observer, ids.pelaporan)).toBeNull()
  })

  it('platform admin bukan grant database', () => {
    expect(accessFor(ids.admin, ids.inventaris)).toBeNull()
  })

  it('mengambil akses tertinggi jika user ada di beberapa group', () => {
    getState().groups.find((group) => group.id === ids.writers)?.members.push(ids.observer)
    expect(accessFor(ids.observer, ids.inventaris)).toBe('view_edit')
  })

  it('group tanpa grant tidak memberi akses', () => {
    getState().groups.find((group) => group.id === ids.magang)?.members.push(ids.pengamat)
    expect(accessFor(ids.pengamat, ids.pelaporan)).toBe('view_only')
    expect(accessFor(ids.editor, ids.arsip)).toBeNull()
  })
})

describe('resetState', () => {
  it('mengembalikan seed awal dan menghapus sesi', () => {
    const state = getState()
    state.users.length = 0
    setSession({ userId: ids.admin, csrfToken: 'x' })
    resetState()
    expect(getState().users).toHaveLength(7)
    expect(getState().session).toBeNull()
  })

  it('token CSRF selalu berbeda', () => {
    expect(newCsrfToken()).not.toBe(newCsrfToken())
  })
})

describe('seed', () => {
  it('setiap leader adalah anggota groupnya dan ada satu group tanpa leader', () => {
    const groups = getState().groups
    for (const group of groups) if (group.leader_user_id) expect(group.members).toContain(group.leader_user_id)
    expect(groups.filter((group) => group.leader_user_id === null)).toHaveLength(1)
  })

  it('setiap grant dan tabel merujuk koneksi dan group yang ada', () => {
    const state = getState()
    for (const grant of state.grants) {
      expect(state.groups.some((group) => group.id === grant.group_id)).toBe(true)
      expect(state.connections.some((connection) => connection.id === grant.database_id)).toBe(true)
    }
    for (const table of state.tables) expect(state.connections.some((connection) => connection.id === table.database_id)).toBe(true)
  })

  it('setiap baris tabel punya jumlah sel yang sama dengan kolomnya', () => {
    for (const table of getState().tables) for (const row of table.rows) expect(row).toHaveLength(table.columns.length)
  })
})
