// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { readNonce } from './csp.ts'

const setMeta = (content: string) => {
  const meta = document.createElement('meta')
  meta.name = 'csp-nonce'
  meta.content = content
  document.head.appendChild(meta)
}

afterEach(() => {
  document.head.querySelectorAll('meta[name="csp-nonce"]').forEach((meta) => meta.remove())
})

describe('readNonce', () => {
  it('membaca nonce yang diisi server dan memangkas spasi', () => {
    setMeta('  abc123+/= ')
    expect(readNonce()).toBe('abc123+/=')
  })

  it('penanda yang belum diganti di dev server diabaikan', () => {
    setMeta('__CSP_NONCE__')
    expect(readNonce()).toBeUndefined()
  })

  it('tanpa meta atau dengan isi kosong tidak ada nonce', () => {
    expect(readNonce()).toBeUndefined()
    setMeta('')
    expect(readNonce()).toBeUndefined()
  })
})
