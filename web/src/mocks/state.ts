import type { Schemas } from '../api/types.ts'
import { createSeed } from './seed.ts'

export type Access = Schemas['Access']
export type Engine = Schemas['Connection']['engine']

export interface MockUser {
  id: string
  username: string
  password: string
  active: boolean
  platform_admin: boolean
}

export interface MockGroup {
  id: string
  name: string
  leader_user_id: string | null
  members: string[]
}

export interface MockConnection {
  id: string
  name: string
  engine: Engine
}

export interface MockGrant {
  group_id: string
  database_id: string
  access: Access
}

export interface MockTable {
  database_id: string
  schema: string
  name: string
  kind: Schemas['TableKind']
  columns: Schemas['Column'][]
  constraints: Schemas['Constraint'][]
  indexes: Schemas['Index'][]
  order: 'primary_key' | 'best_effort'
  rows: (string | null)[][]
}

export interface MockSession {
  userId: string
  csrfToken: string
}

export type MockMigration = Schemas['Migration']
export type MockChangeRequest = Schemas['ChangeRequest']

// Skenario hasil job dipilih dari komentar SQL seperti -- mock:fail agar setiap status akhir bisa dicoba tanpa backend
export type Scenario = 'ok' | 'fail' | 'retry' | 'partial' | 'uncertain'

export interface JobPlan {
  direction: Schemas['Direction']
  sql: string
  scenario: Scenario
  batches: number[]
  done: number
  totalKnown: boolean
  compensate: boolean
  resume: boolean
  uncertainAt: number | null
}

export type MockJob = Schemas['Job'] & {
  attempts: Schemas['JobAttempt'][]
  plan: JobPlan
  ticks: number
  resolved: boolean
}

export type MockExecution = Schemas['Execution'] & {
  statements: Schemas['ExecutedStatement'][]
}

export interface MockState {
  users: MockUser[]
  groups: MockGroup[]
  connections: MockConnection[]
  grants: MockGrant[]
  tables: MockTable[]
  migrations: MockMigration[]
  changeRequests: MockChangeRequest[]
  jobs: MockJob[]
  executions: MockExecution[]
  positionVersions: Record<string, number>
  session: MockSession | null
  loginFailures: number
  lockedUntil: number
}

export const loginFailureLimit = 5
export const loginLockMs = 60_000

const storageKey = 'tocatdz-mock-session'
let tokenCounter = 0

// Sesi disimpan di sessionStorage supaya tidak hilang saat halaman dimuat ulang di mode dev
function readStoredSession(): MockSession | null {
  try {
    const raw = globalThis.sessionStorage?.getItem(storageKey)
    const parsed: unknown = raw ? JSON.parse(raw) : null
    if (parsed && typeof parsed === 'object' && 'userId' in parsed && 'csrfToken' in parsed) return parsed as MockSession
  } catch {
    // Penyimpanan tidak tersedia atau isinya rusak
  }
  return null
}

function writeStoredSession(session: MockSession | null) {
  try {
    if (session) globalThis.sessionStorage?.setItem(storageKey, JSON.stringify(session))
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // Penyimpanan diblokir browser
  }
}

function freshState(): MockState {
  const state = createSeed()
  const stored = readStoredSession()
  if (stored && state.users.some((user) => user.id === stored.userId && user.active)) state.session = stored
  return state
}

let state = freshState()

export function getState(): MockState {
  return state
}

// Mengembalikan semua data ke kondisi awal, dipakai antar test
export function resetState() {
  writeStoredSession(null)
  setClock(null)
  state = freshState()
}

let clock: () => Date = () => new Date()

// Jam bisa diganti di test supaya waktu pada job dan riwayat deterministik
export function setClock(next: (() => Date) | null) {
  clock = next ?? (() => new Date())
}

export function now(): string {
  return clock().toISOString().replace(/\.\d{3}Z$/, 'Z')
}

export function setSession(session: MockSession | null) {
  state.session = session
  writeStoredSession(session)
}

export function newCsrfToken(): string {
  tokenCounter += 1
  return `mock-csrf-${tokenCounter}-${crypto.randomUUID()}`
}

// Akses efektif adalah yang tertinggi dari semua group tempat user menjadi anggota
export function accessFor(userId: string, databaseId: string): Access | null {
  let best: Access | null = null
  for (const group of state.groups) {
    if (!group.members.includes(userId)) continue
    for (const grant of state.grants) {
      if (grant.group_id !== group.id || grant.database_id !== databaseId) continue
      if (grant.access === 'view_edit') return 'view_edit'
      best = 'view_only'
    }
  }
  return best
}
