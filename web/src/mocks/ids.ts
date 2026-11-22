const uuid = (kind: number, n: number) => `${kind}0000000-0000-4000-8000-${String(n).padStart(12, '0')}`

export const ids = {
  reviewer: uuid(3, 1),
  observer: uuid(3, 2),
  editor: uuid(3, 3),
  admin: uuid(3, 4),
  auditor: uuid(3, 5),
  pengamat: uuid(3, 6),
  nonaktif: uuid(3, 7),
  writers: uuid(4, 1),
  readers: uuid(4, 2),
  auditors: uuid(4, 3),
  magang: uuid(4, 4),
  inventaris: uuid(1, 1),
  pelaporan: uuid(1, 2),
  arsip: uuid(1, 3),
} as const

// Migration A sampai C sudah dieksekusi, D dan E masih draft, dan P milik database pelaporan
export const workflowIds = {
  migrationA: uuid(2, 1),
  migrationB: uuid(2, 2),
  migrationC: uuid(2, 3),
  migrationD: uuid(2, 4),
  migrationE: uuid(2, 5),
  migrationP: uuid(2, 6),
  changeRequest: (n: number) => uuid(5, n),
  job: (n: number) => uuid(6, n),
  execution: (n: number) => uuid(7, n),
}
