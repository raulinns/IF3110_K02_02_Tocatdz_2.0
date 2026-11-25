import { useMemo } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate } from 'react-router-dom'
import { createMigration, migrationQuery, type Definition } from '../../api/migrations.ts'
import { Breadcrumbs } from '../../components/ui/Breadcrumbs.tsx'
import { ErrorPage } from '../../components/ui/ErrorPage.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { emptyForm } from '../../lib/definition.ts'
import { migrationPath, migrationsPath } from '../../lib/paths.ts'
import { useDatabase } from '../../lib/useDatabase.ts'
import { DefinitionEditor } from './DefinitionEditor.tsx'
import { readPrefill } from './prefill.ts'

export function MigrationNewPage() {
  const { db, database, canEdit } = useDatabase()
  const client = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const prefill = useMemo(() => readPrefill(location.state), [location.state])
  const initial = useMemo(() => prefill?.form ?? emptyForm(), [prefill])

  if (!database) return null
  // Hanya yang boleh mengubah database yang dapat membuat draft, dan backend tetap memeriksanya
  if (!canEdit) return <ErrorPage status={403} />

  async function save(definition: Definition) {
    const created = await createMigration(db, definition)
    client.setQueryData(migrationQuery(db, created.id).queryKey, created)
    void client.invalidateQueries({ queryKey: ['databases', db, 'migrations'] })
    void navigate(migrationPath(db, created.id), { replace: true, state: { notice: 'Draft berhasil dibuat.' } })
  }

  return (
    <>
      <Breadcrumbs items={[{ label: 'Migration', to: migrationsPath(db) }, { label: 'Draft baru' }]} />
      <h2 className="h5 mb-3">Draft baru</h2>
      {prefill?.source === 'diff' && <Notice kind="info">SQL up berasal dari schema diff. Tinjau setiap perintah, lalu isi SQL down atau tandai migration sebagai irreversible sebelum menyimpan.</Notice>}
      {prefill?.source === 'copy' && <Notice kind="info">Salinan dari migration {prefill.origin ? <strong>{prefill.origin}</strong> : 'sebelumnya'}. Draft baru bisa diubah dan diajukan sendiri.</Notice>}
      <div className="card">
        <div className="card-body">
          <DefinitionEditor initial={initial} readOnly={false} submitLabel="Simpan draft" onSave={save} cancelTo={migrationsPath(db)} />
        </div>
      </div>
    </>
  )
}
