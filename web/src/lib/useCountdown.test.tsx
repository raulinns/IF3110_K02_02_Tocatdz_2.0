// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useCountdown } from './useCountdown.ts'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('useCountdown', () => {
  it('awalnya nol dan menghitung mundur per detik sampai berhenti di nol', () => {
    const { result } = renderHook(() => useCountdown())
    expect(result.current.remaining).toBe(0)
    act(() => result.current.start(3))
    expect(result.current.remaining).toBe(3)
    act(() => vi.advanceTimersByTime(1000))
    expect(result.current.remaining).toBe(2)
    act(() => vi.advanceTimersByTime(1000))
    expect(result.current.remaining).toBe(1)
    act(() => vi.advanceTimersByTime(5000))
    expect(result.current.remaining).toBe(0)
  })

  it('membulatkan ke atas sehingga sisa waktu tidak tampil nol sebelum tenggat', () => {
    const { result } = renderHook(() => useCountdown())
    act(() => result.current.start(2))
    act(() => vi.advanceTimersByTime(1750))
    expect(result.current.remaining).toBe(1)
  })

  it('bisa dimulai lagi setelah selesai dan memulai ulang saat masih berjalan', () => {
    const { result } = renderHook(() => useCountdown())
    act(() => result.current.start(1))
    act(() => vi.advanceTimersByTime(2000))
    expect(result.current.remaining).toBe(0)
    act(() => result.current.start(5))
    expect(result.current.remaining).toBe(5)
    act(() => vi.advanceTimersByTime(2000))
    act(() => result.current.start(10))
    expect(result.current.remaining).toBe(10)
  })

  it('timer dibersihkan saat komponen dilepas', () => {
    const { result, unmount } = renderHook(() => useCountdown())
    act(() => result.current.start(10))
    expect(vi.getTimerCount()).toBe(1)
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
