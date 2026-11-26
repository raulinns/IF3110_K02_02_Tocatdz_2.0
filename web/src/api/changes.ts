import { api, path } from './client.ts'
import { ApiError, isApiError } from './errors.ts'
import type { Schemas } from './types.ts'

const notSubmittable = 'Permintaan belum bisa diajukan. Periksa bahwa database ini punya group yang berhak dan setiap group sudah memiliki leader, lalu pastikan migration-nya memang bisa dijalankan.'
const busy = 'Permintaan tidak dapat diajukan karena data sudah berubah, atau database ini masih punya permintaan atau job yang belum selesai. Muat ulang lalu coba lagi.'

// Server sungguhan tidak mengirim reason, sehingga penyebab yang paling mungkin dijelaskan dari kode galatnya
function explainRejection(error: unknown): never {
  if (isApiError(error) && error.reason === undefined) {
    const message = error.code === 'conflict' ? busy : error.code === 'validation_failed' && Object.keys(error.fields).length === 0 ? notSubmittable : undefined
    if (message) throw new ApiError({ status: error.status, code: error.code, message, requestId: error.requestId, fields: error.fields })
  }
  throw error
}

// Mengajukan definisi migration untuk direview, dan definisinya dibekukan di server dalam transaksi yang sama
export const submitChange = (db: string, input: Schemas['SubmitChangeInput']) => api.post<Schemas['ChangeRequest']>(path`/databases/${db}/change-requests`, input).catch(explainRejection)

export const submitApply = (db: string, migration: Pick<Schemas['Migration'], 'id' | 'revision'>) => submitChange(db, { migration_id: migration.id, migration_revision: migration.revision, kind: 'apply' })
