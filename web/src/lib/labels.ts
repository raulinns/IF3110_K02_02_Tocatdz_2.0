import type { Schemas } from '../api/types.ts'

export const accessLabel = (access: Schemas['Access']) => (access === 'view_edit' ? 'Lihat + ubah' : 'Lihat saja')

export const engineLabel = (engine: Schemas['Database']['engine']) => (engine === 'mysql' ? 'MySQL' : 'PostgreSQL')

export const tableKindLabel = (kind: Schemas['TableKind']) => kind.replace(/_/g, ' ')
