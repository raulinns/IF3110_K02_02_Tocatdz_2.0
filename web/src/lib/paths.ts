const encode = encodeURIComponent

// Nama schema dan tabel bisa memuat spasi atau garis miring sehingga setiap segmen harus di-encode
export const databasePath = (db: string) => `/databases/${encode(db)}`
export const schemaPath = (db: string) => `${databasePath(db)}/schema`
export const tablePath = (db: string, schema: string, table: string) => `${schemaPath(db)}/${encode(schema)}/${encode(table)}`
export const adminGroupPath = (groupId: string) => `/admin/groups/${encode(groupId)}`
export const rowsPath = (db: string, schema: string, table: string) => `${tablePath(db, schema, table)}/data`
