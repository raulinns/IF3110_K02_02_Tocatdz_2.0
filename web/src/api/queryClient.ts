import { QueryClient } from '@tanstack/react-query'
import { shouldRetryQuery } from './retry.ts'

// networkMode always membuat permintaan tetap dicoba saat browser offline, sehingga kegagalannya tampil sebagai galat jaringan dengan tombol coba lagi
// Dengan mode bawaan, query dan mutasi hanya dijeda diam-diam sampai koneksi kembali dan pengguna tidak melihat apa pun
export const queryDefaults = {
  queries: { retry: shouldRetryQuery, refetchOnWindowFocus: false, networkMode: 'always' as const },
  mutations: { retry: false, networkMode: 'always' as const },
}

export const createQueryClient = () => new QueryClient({ defaultOptions: queryDefaults })
