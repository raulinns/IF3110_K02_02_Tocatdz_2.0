// @vitest-environment jsdom
import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderApp } from '../../test/dom.tsx'
import { SqlField } from './SqlField.tsx'

// Berkas terpisah supaya dokumen jsdom-nya baru dan gaya editor belum pernah dipasang oleh test lain
describe('editor SQL dan CSP', () => {
  it('tag style yang disuntikkan CodeMirror membawa nonce dari halaman', async () => {
    const meta = document.createElement('meta')
    meta.name = 'csp-nonce'
    meta.content = 'nonce-uji-123'
    document.head.appendChild(meta)
    renderApp(<SqlField label="SQL" value="SELECT 1;" />)
    await screen.findByRole('textbox', { name: 'SQL' })
    const styles = Array.from(document.head.querySelectorAll('style'))
    expect(styles.length).toBeGreaterThan(0)
    expect(styles.every((style) => style.getAttribute('nonce') === 'nonce-uji-123')).toBe(true)
  })
})
