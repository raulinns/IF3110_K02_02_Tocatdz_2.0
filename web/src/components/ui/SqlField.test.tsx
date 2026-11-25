// @vitest-environment jsdom
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { renderApp } from '../../test/dom.tsx'
import { setSql, sqlOf } from '../../test/editor.ts'
import { SqlField } from './SqlField.tsx'

function Harness({ readOnly = false, errors }: { readOnly?: boolean; errors?: string[] }) {
  const [value, setValue] = useState('SELECT 1;')
  return (
    <>
      <SqlField label="SQL up" value={value} onChange={setValue} readOnly={readOnly} errors={errors} hint="Satu transaction" />
      <output aria-label="nilai">{value}</output>
    </>
  )
}

describe('editor SQL', () => {
  it('tampil sebagai kotak teks bernama dengan isi awal', async () => {
    renderApp(<Harness />)
    expect(await screen.findByRole('textbox', { name: 'SQL up' })).toBeInTheDocument()
    expect(await sqlOf('SQL up')).toBe('SELECT 1;')
  })

  it('perubahan isi editor diteruskan ke pemiliknya', async () => {
    renderApp(<Harness />)
    await setSql('SQL up', 'DROP TABLE a;')
    expect(screen.getByLabelText('nilai')).toHaveTextContent('DROP TABLE a;')
  })

  it('error dan petunjuk dihubungkan ke editor lewat aria-describedby', async () => {
    renderApp(<Harness errors={['SQL tidak valid']} />)
    const box = await screen.findByRole('textbox', { name: 'SQL up' })
    expect(box).toHaveAttribute('aria-invalid', 'true')
    expect(box).toHaveAccessibleDescription('SQL tidak valid Satu transaction')
    expect(screen.getByText('SQL tidak valid')).toBeInTheDocument()
  })

  it('beberapa error ditampilkan sebagai daftar', async () => {
    renderApp(<Harness errors={['Pesan satu', 'Pesan dua']} />)
    await screen.findByRole('textbox', { name: 'SQL up' })
    expect(screen.getAllByRole('listitem').map((item) => item.textContent)).toEqual(['Pesan satu', 'Pesan dua'])
  })

  it('mode baca saja menandai editor dan menolak ketikan', async () => {
    const user = userEvent.setup()
    renderApp(<Harness readOnly />)
    const box = await screen.findByRole('textbox', { name: 'SQL up' })
    expect(box).toHaveAttribute('aria-readonly', 'true')
    await user.click(box)
    await user.keyboard('abc')
    expect(await sqlOf('SQL up')).toBe('SELECT 1;')
  })

  it('perubahan nilai dari luar masuk ke editor tanpa membuat ulang editor', async () => {
    function Outer() {
      const [value, setValue] = useState('a')
      return (
        <>
          <button type="button" onClick={() => setValue('b')}>
            ganti
          </button>
          <SqlField label="SQL" value={value} onChange={setValue} />
        </>
      )
    }
    const user = userEvent.setup()
    renderApp(<Outer />)
    const before = await screen.findByRole('textbox', { name: 'SQL' })
    await user.click(screen.getByRole('button', { name: 'ganti' }))
    expect(await sqlOf('SQL')).toBe('b')
    expect(screen.getByRole('textbox', { name: 'SQL' })).toBe(before)
  })
})
