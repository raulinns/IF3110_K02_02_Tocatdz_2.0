import type { Schemas } from '../api/types.ts'
import { ids } from './ids.ts'
import type { MockState, MockTable } from './state.ts'
import { workflowSeed } from './seed-workflow.ts'

export { ids }

// Semua akun demo memakai password yang sama
export const demoPassword = 'tocatdz-demo-2026'

type Row = (string | null)[]
type ColumnSpec = [name: string, type: string, nullable?: boolean, fallback?: string | null]

interface TableOptions {
  kind?: Schemas['TableKind']
  primaryKey?: string | null
  constraints?: Schemas['Constraint'][]
  indexes?: Schemas['Index'][]
}

const range = (count: number, row: (n: number) => Row): Row[] => Array.from({ length: count }, (_, i) => row(i + 1))
const pad = (value: number) => String(value).padStart(2, '0')
const timestamp = (n: number) => `2026-10-${pad(1 + (n % 28))} ${pad(8 + (n % 10))}:${pad((n * 7) % 60)}:00+00`
const price = (productId: number) => 15000 + productId * 12500

function makeTable(databaseId: string, schema: string, name: string, columns: ColumnSpec[], rows: Row[], options: TableOptions = {}): MockTable {
  const kind = options.kind ?? 'table'
  const physical = kind === 'table'
  const primaryKey = options.primaryKey === undefined ? columns[0][0] : options.primaryKey
  const keyed = physical && primaryKey !== null
  return {
    database_id: databaseId,
    schema,
    name,
    kind,
    columns: columns.map(([columnName, dataType, nullable = false, fallback = null]) => ({ name: columnName, data_type: dataType, nullable, default: fallback })),
    constraints: physical ? [...(keyed ? [{ name: `${name}_pkey`, type: 'PRIMARY KEY', definition: `PRIMARY KEY (${primaryKey})` }] : []), ...(options.constraints ?? [])] : [],
    indexes: physical
      ? [
          ...(keyed ? [{ name: `${name}_pkey`, definition: `CREATE UNIQUE INDEX ${name}_pkey ON ${schema}.${name} USING btree (${primaryKey})`, unique: true, primary: true }] : []),
          ...(options.indexes ?? []),
        ]
      : [],
    order: keyed ? 'primary_key' : 'best_effort',
    rows,
  }
}

function inventarisTables(): MockTable[] {
  const db = ids.inventaris
  const names = ['Kabel HDMI', 'Mouse Nirkabel', 'Keyboard Mekanik', 'Monitor 24 inci', 'Headset', 'Webcam HD', 'Flashdisk 64GB', 'SSD 512GB']
  const cities = ['Bandung', 'Jakarta', 'Surabaya', 'Medan', 'Makassar']
  const statuses = ['baru', 'dibayar', 'dikirim', 'selesai']
  const products = range(24, (n) => [String(n), `SKU-${String(n).padStart(4, '0')}`, `${names[n % names.length]} ${Math.ceil(n / names.length)}`, price(n).toFixed(2), String((n * 7) % 31), n % 6 === 0 ? 'false' : 'true', timestamp(n)])
  const lowStock = products.filter((row) => Number(row[4]) < 10).map((row) => [row[1], row[2], row[4]])
  return [
    makeTable(
      db,
      'public',
      'products',
      [
        ['id', 'bigint'],
        ['sku', 'text'],
        ['nama', 'text'],
        ['harga', 'numeric(12,2)'],
        ['stok', 'integer', false, '0'],
        ['aktif', 'boolean', false, 'true'],
        ['dibuat', 'timestamp with time zone', false, 'now()'],
      ],
      products,
      { constraints: [{ name: 'products_sku_key', type: 'UNIQUE', definition: 'UNIQUE (sku)' }], indexes: [{ name: 'products_sku_key', definition: 'CREATE UNIQUE INDEX products_sku_key ON public.products USING btree (sku)', unique: true, primary: false }] },
    ),
    makeTable(
      db,
      'public',
      'customers',
      [
        ['id', 'bigint'],
        ['nama', 'text'],
        ['email', 'text', true],
        ['kota', 'text', true],
        ['dibuat', 'timestamp with time zone', false, 'now()'],
      ],
      range(60, (n) => [String(n), `Pelanggan ${n}`, n % 5 === 0 ? null : `pelanggan${n}@contoh.id`, n % 7 === 0 ? null : cities[n % cities.length], timestamp(n)]),
    ),
    makeTable(
      db,
      'public',
      'orders',
      [
        ['id', 'bigint'],
        ['customer_id', 'bigint'],
        ['product_id', 'bigint'],
        ['jumlah', 'integer', false, '1'],
        ['total', 'numeric(12,2)'],
        ['status', 'text', false, "'baru'::text"],
        ['dibuat', 'timestamp with time zone', false, 'now()'],
      ],
      range(137, (n) => {
        const productId = ((n * 5) % 24) + 1
        const quantity = (n % 5) + 1
        return [String(n), String(((n * 3) % 60) + 1), String(productId), String(quantity), (price(productId) * quantity).toFixed(2), statuses[n % statuses.length], timestamp(n)]
      }),
      {
        constraints: [
          { name: 'orders_customer_id_fkey', type: 'FOREIGN KEY', definition: 'FOREIGN KEY (customer_id) REFERENCES public.customers(id)' },
          { name: 'orders_product_id_fkey', type: 'FOREIGN KEY', definition: 'FOREIGN KEY (product_id) REFERENCES public.products(id)' },
          { name: 'orders_jumlah_check', type: 'CHECK', definition: 'CHECK ((jumlah > 0))' },
        ],
        indexes: [{ name: 'idx_orders_customer', definition: 'CREATE INDEX idx_orders_customer ON public.orders USING btree (customer_id)', unique: false, primary: false }],
      },
    ),
    makeTable(
      db,
      'public',
      'v_stok_menipis',
      [
        ['sku', 'text'],
        ['nama', 'text'],
        ['stok', 'integer'],
      ],
      lowStock,
      { kind: 'view', primaryKey: null },
    ),
    makeTable(
      db,
      'audit',
      'log_entries',
      [
        ['waktu', 'timestamp with time zone'],
        ['aktor', 'text'],
        ['aksi', 'text'],
        ['detail', 'text', true],
        ['tanda_tangan', 'bytea'],
      ],
      range(12, (n) => [timestamp(n), n % 2 ? 'reviewer' : 'editor', n % 3 ? 'ubah' : 'hapus', n === 3 ? 'x'.repeat(5000) : n % 4 === 0 ? null : `Perubahan nomor ${n}`, `\\x${n.toString(16).padStart(8, '0')}`]),
      { primaryKey: null },
    ),
  ]
}

function pelaporanTables(): MockTable[] {
  const db = ids.pelaporan
  return [
    makeTable(
      db,
      'public',
      'laporan_harian',
      [
        ['id', 'bigint'],
        ['tanggal', 'date'],
        ['total_order', 'integer'],
        ['pendapatan', 'numeric(14,2)'],
      ],
      range(30, (n) => [String(n), `2026-10-${pad(n)}`, String(20 + ((n * 13) % 40)), ((20 + ((n * 13) % 40)) * 185000).toFixed(2)]),
    ),
    makeTable(
      db,
      'public',
      'metrik',
      [
        ['nama', 'text'],
        ['nilai', 'numeric'],
        ['diperbarui', 'timestamp with time zone', false, 'now()'],
      ],
      range(15, (n) => [`metrik_${pad(n)}`, String(n * 3.5), timestamp(n)]),
    ),
  ]
}

function arsipTables(): MockTable[] {
  const db = ids.arsip
  return [
    makeTable(
      db,
      'arsip',
      'dokumen',
      [
        ['id', 'int'],
        ['judul', 'varchar(200)'],
        ['kategori_id', 'int'],
        ['ukuran_kb', 'int', true],
        ['dibuat', 'datetime'],
      ],
      range(40, (n) => [String(n), `Dokumen ${n}`, String((n % 8) + 1), n % 9 === 0 ? null : String(n * 12), timestamp(n).slice(0, 19)]),
      { constraints: [{ name: 'fk_dokumen_kategori', type: 'FOREIGN KEY', definition: 'FOREIGN KEY (kategori_id) REFERENCES kategori (id)' }] },
    ),
    makeTable(
      db,
      'arsip',
      'kategori',
      [
        ['id', 'int'],
        ['nama', 'varchar(100)'],
      ],
      range(8, (n) => [String(n), `Kategori ${n}`]),
    ),
  ]
}

export function createSeed(): MockState {
  return {
    users: [
      { id: ids.reviewer, username: 'reviewer', password: demoPassword, active: true, platform_admin: false },
      { id: ids.observer, username: 'observer', password: demoPassword, active: true, platform_admin: false },
      { id: ids.editor, username: 'editor', password: demoPassword, active: true, platform_admin: false },
      { id: ids.admin, username: 'admin', password: demoPassword, active: true, platform_admin: true },
      { id: ids.auditor, username: 'auditor', password: demoPassword, active: true, platform_admin: false },
      { id: ids.pengamat, username: 'pengamat', password: demoPassword, active: true, platform_admin: false },
      { id: ids.nonaktif, username: 'nonaktif', password: demoPassword, active: false, platform_admin: false },
    ],
    groups: [
      { id: ids.writers, name: 'writers', leader_user_id: ids.reviewer, members: [ids.reviewer, ids.editor] },
      { id: ids.readers, name: 'readers', leader_user_id: ids.observer, members: [ids.observer, ids.pengamat] },
      { id: ids.auditors, name: 'auditors', leader_user_id: ids.auditor, members: [ids.auditor] },
      { id: ids.magang, name: 'magang', leader_user_id: null, members: [] },
    ],
    connections: [
      { id: ids.inventaris, name: 'inventaris', engine: 'postgresql' },
      { id: ids.pelaporan, name: 'pelaporan', engine: 'postgresql' },
      { id: ids.arsip, name: 'arsip', engine: 'mysql' },
    ],
    grants: [
      { group_id: ids.writers, database_id: ids.inventaris, access: 'view_edit' },
      { group_id: ids.readers, database_id: ids.inventaris, access: 'view_only' },
      { group_id: ids.writers, database_id: ids.pelaporan, access: 'view_edit' },
      { group_id: ids.auditors, database_id: ids.pelaporan, access: 'view_only' },
      { group_id: ids.magang, database_id: ids.pelaporan, access: 'view_only' },
      { group_id: ids.readers, database_id: ids.arsip, access: 'view_only' },
    ],
    tables: [...inventarisTables(), ...pelaporanTables(), ...arsipTables()],
    ...workflowSeed(),
    session: null,
    loginFailures: 0,
    lockedUntil: 0,
  }
}
