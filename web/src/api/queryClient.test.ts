// @vitest-environment jsdom
import { onlineManager } from '@tanstack/react-query'
import { afterEach, describe, expect, it } from 'vitest'
import { ApiError } from './errors.ts'
import { createQueryClient, queryDefaults } from './queryClient.ts'

afterEach(() => onlineManager.setOnline(true))

describe('klien query', () => {
  it('tetap mencoba permintaan saat browser offline untuk query dan mutasi', () => {
    expect(queryDefaults.queries.networkMode).toBe('always')
    expect(queryDefaults.mutations.networkMode).toBe('always')
  })

  it('query yang gagal saat offline berakhir sebagai galat jaringan dan tidak menggantung', async () => {
    onlineManager.setOnline(false)
    const client = createQueryClient()
    client.setDefaultOptions({ ...client.getDefaultOptions(), queries: { ...client.getDefaultOptions().queries, retry: false } })
    const failure = new ApiError({ status: 0, code: 'network', message: 'Tidak dapat terhubung ke server' })
    await expect(client.fetchQuery({ queryKey: ['offline'], queryFn: () => Promise.reject(failure) })).rejects.toBe(failure)
  })
})
