import { adminHandlers } from './admin.ts'
import { authHandlers } from './auth.ts'
import { explorerHandlers } from './explorer.ts'

export const handlers = [...authHandlers, ...adminHandlers, ...explorerHandlers]
