import { api, path } from './client.ts'
import type { Schemas } from './types.ts'

// Mengajukan definisi migration untuk direview, dan definisinya dibekukan di server dalam transaksi yang sama
export const submitApply = (db: string, migration: Pick<Schemas['Migration'], 'id' | 'revision'>) =>
  api.post<Schemas['ChangeRequest']>(path`/databases/${db}/change-requests`, { migration_id: migration.id, migration_revision: migration.revision, kind: 'apply' })
