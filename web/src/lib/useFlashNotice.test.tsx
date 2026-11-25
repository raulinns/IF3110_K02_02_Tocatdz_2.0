// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import '../test/dom.tsx'
import { readNotice, useFlashNotice } from './useFlashNotice.ts'

function Probe() {
  const [notice] = useFlashNotice()
  const location = useLocation()
  return (
    <>
      <output aria-label="pesan">{notice ?? 'kosong'}</output>
      <output aria-label="state">{JSON.stringify(location.state)}</output>
    </>
  )
}

const open = (state: unknown) =>
  render(
    <MemoryRouter initialEntries={[{ pathname: '/x', state }]}>
      <Routes>
        <Route path="/x" element={<Probe />} />
      </Routes>
    </MemoryRouter>,
  )

describe('readNotice', () => {
  it('hanya menerima state berbentuk objek dengan pesan teks', () => {
    expect(readNotice({ notice: 'halo' })).toBe('halo')
    expect(readNotice({ notice: 5 })).toBeNull()
    expect(readNotice(null)).toBeNull()
    expect(readNotice('halo')).toBeNull()
    expect(readNotice(undefined)).toBeNull()
  })
})

describe('useFlashNotice', () => {
  it('menampilkan pesan dari state lalu menghapusnya dari riwayat', async () => {
    open({ notice: 'Berhasil dibuat' })
    expect(screen.getByLabelText('pesan')).toHaveTextContent('Berhasil dibuat')
    expect(await screen.findByText('null', { selector: 'output' })).toBeInTheDocument()
  })

  it('tanpa pesan tidak mengubah apa pun', () => {
    open(undefined)
    expect(screen.getByLabelText('pesan')).toHaveTextContent('kosong')
  })
})
