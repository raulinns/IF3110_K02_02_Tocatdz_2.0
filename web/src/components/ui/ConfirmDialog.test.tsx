// @vitest-environment jsdom
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/errors.ts'
import { renderApp } from '../../test/dom.tsx'
import { ConfirmDialog } from './ConfirmDialog.tsx'

function show(onConfirm: () => Promise<unknown>, onClose = vi.fn()) {
  const user = userEvent.setup()
  renderApp(
    <ConfirmDialog title="Hapus data" confirmLabel="Ya, hapus" danger onConfirm={onConfirm} onClose={onClose}>
      Data akan dihapus.
    </ConfirmDialog>,
  )
  return { user, onClose }
}

describe('dialog konfirmasi', () => {
  it('menampilkan judul, pesan, dan fokus awal di tombol Batal', () => {
    show(() => Promise.resolve())
    expect(screen.getByRole('dialog', { name: 'Hapus data' })).toBeInTheDocument()
    expect(screen.getByText('Data akan dihapus.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Batal' })).toHaveFocus()
    expect(screen.getByRole('button', { name: 'Ya, hapus' })).toHaveClass('btn-danger')
  })

  it('Batal menutup tanpa menjalankan aksi', async () => {
    const onConfirm = vi.fn(() => Promise.resolve())
    const { user, onClose } = show(onConfirm)
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(onConfirm).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('konfirmasi menjalankan aksi sekali lalu menutup dialog', async () => {
    const onConfirm = vi.fn(() => Promise.resolve())
    const { user, onClose } = show(onConfirm)
    await user.click(screen.getByRole('button', { name: 'Ya, hapus' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('selama aksi berjalan tombol dikunci sehingga klik ganda tidak mengirim dua kali', async () => {
    let finish: () => void = () => undefined
    const onConfirm = vi.fn(() => new Promise<void>((resolve) => (finish = resolve)))
    const { user, onClose } = show(onConfirm)
    await user.click(screen.getByRole('button', { name: 'Ya, hapus' }))
    expect(screen.getByRole('button', { name: 'Ya, hapus' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Batal' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Ya, hapus' }))
    await user.keyboard('{Escape}')
    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(onClose).not.toHaveBeenCalled()
    finish()
    await vi.waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  it('jika gagal dialog tetap terbuka dengan pesan error dan bisa dicoba lagi', async () => {
    const onConfirm = vi
      .fn<() => Promise<void>>()
      .mockRejectedValueOnce(new ApiError({ status: 409, code: 'conflict', message: 'Group masih dipakai' }))
      .mockResolvedValueOnce(undefined)
    const { user, onClose } = show(onConfirm)
    await user.click(screen.getByRole('button', { name: 'Ya, hapus' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Group masih dipakai')
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Ya, hapus' })).toBeEnabled()
    await user.click(screen.getByRole('button', { name: 'Ya, hapus' }))
    expect(onConfirm).toHaveBeenCalledTimes(2)
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
