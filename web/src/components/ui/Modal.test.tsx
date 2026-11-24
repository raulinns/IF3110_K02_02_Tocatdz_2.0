// @vitest-environment jsdom
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/dom.tsx'
import { Modal } from './Modal.tsx'

function Harness({ busy = false, onClose = () => undefined }: { busy?: boolean; onClose?: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Buka
      </button>
      {open && (
        <Modal
          title="Judul dialog"
          busy={busy}
          onClose={() => {
            onClose()
            setOpen(false)
          }}
          footer={<button type="button">Simpan</button>}
        >
          <input aria-label="Isian" />
        </Modal>
      )}
    </>
  )
}

async function open(props: Parameters<typeof Harness>[0] = {}) {
  const user = userEvent.setup()
  const view = renderApp(<Harness {...props} />)
  await user.click(screen.getByRole('button', { name: 'Buka' }))
  return { user, ...view }
}

describe('modal', () => {
  it('tampil sebagai dialog bernama dengan fokus awal di isian pertama', async () => {
    await open()
    const dialog = screen.getByRole('dialog', { name: 'Judul dialog' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByLabelText('Isian')).toHaveFocus()
  })

  it('isi halaman di belakang dinonaktifkan lalu diaktifkan lagi saat ditutup', async () => {
    const { user, container } = await open()
    expect(container).toHaveAttribute('inert')
    expect(document.body).toHaveClass('modal-open')
    await user.keyboard('{Escape}')
    expect(container).not.toHaveAttribute('inert')
    expect(document.body).not.toHaveClass('modal-open')
  })

  it('fokus tidak keluar dari dialog saat Tab dan Shift+Tab', async () => {
    const { user } = await open()
    // Urutan fokus di dalam dialog: tombol Tutup, isian, lalu Simpan
    await user.tab()
    expect(screen.getByRole('button', { name: 'Simpan' })).toHaveFocus()
    await user.tab()
    expect(screen.getByRole('button', { name: 'Tutup' })).toHaveFocus()
    await user.tab({ shift: true })
    expect(screen.getByRole('button', { name: 'Simpan' })).toHaveFocus()
  })

  it('Escape menutup dan fokus kembali ke tombol pemicu', async () => {
    const onClose = vi.fn()
    const { user } = await open({ onClose })
    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Buka' })).toHaveFocus()
  })

  it('klik latar menutup tetapi klik di dalam dialog tidak', async () => {
    const onClose = vi.fn()
    const { user } = await open({ onClose })
    await user.click(screen.getByRole('dialog'))
    expect(onClose).not.toHaveBeenCalled()
    await user.click(document.querySelector('.modal') as HTMLElement)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('tombol Tutup menutup dialog', async () => {
    const onClose = vi.fn()
    const { user } = await open({ onClose })
    await user.click(screen.getByRole('button', { name: 'Tutup' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('selama sibuk dialog tidak bisa ditutup lewat Escape, latar, maupun tombol Tutup', async () => {
    const onClose = vi.fn()
    const { user } = await open({ busy: true, onClose })
    await user.keyboard('{Escape}')
    await user.click(document.querySelector('.modal') as HTMLElement)
    expect(screen.getByRole('button', { name: 'Tutup' })).toBeDisabled()
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })
})
