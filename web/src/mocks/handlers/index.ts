import { adminHandlers } from './admin.ts'
import { authHandlers } from './auth.ts'
import { changeHandlers } from './changes.ts'
import { explorerHandlers } from './explorer.ts'
import { historyHandlers } from './history.ts'
import { jobHandlers } from './jobs.ts'
import { migrationHandlers } from './migrations.ts'

export const handlers = [...authHandlers, ...adminHandlers, ...explorerHandlers, ...migrationHandlers, ...changeHandlers, ...jobHandlers, ...historyHandlers]
