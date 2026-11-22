import { afterEach, describe, expect, it } from 'vitest'
import { ids } from './seed.ts'
import { signInFromUrl } from './quick-login.ts'
import { getState, resetState } from './state.ts'

afterEach(() => resetState())

describe('signInFromUrl', () => {
  it('masuk sebagai user seed yang disebut di parameter as', () => {
    expect(signInFromUrl('?as=reviewer')).toBe(true)
    expect(getState().session?.userId).toBe(ids.reviewer)
    expect(getState().session?.csrfToken).toBeTruthy()
  })

  it('mengabaikan user yang tidak ada, nonaktif, atau parameter yang kosong', () => {
    expect(signInFromUrl('?as=tidak-ada')).toBe(false)
    expect(signInFromUrl('?as=nonaktif')).toBe(false)
    expect(signInFromUrl('')).toBe(false)
    expect(getState().session).toBeNull()
  })
})
