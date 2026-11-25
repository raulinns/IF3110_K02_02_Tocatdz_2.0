import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { migrationQuery, updateMigration, type Definition } from '../../api/migrations.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Breadcrumbs } from '../../components/ui/Breadcrumbs.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { formFromMigration } from '../../lib/definition.ts'
import { definitionStateLabel, modeLabel } from '../../lib/labels.ts'
import { migrationsPath, newMigrationPath, reviewPath } from '../../lib/paths.ts'
import { useDatabase } from '../../lib/useDatabase.ts'
import { DefinitionEditor } from './DefinitionEditor.tsx'
import { PlanPanel } from './PlanPanel.tsx'
import { readNotice } from './prefill.ts'
import { SubmitDialog } from './SubmitDialog.tsx'

export function MigrationDetailPage() {
  const { db, canEdit } = useDatabase()
  const { migrationId = '' } = useParams()
  const client = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const query = useQuery(migrationQuery(db, migrationId))
  const [notice, setNotice] = useState<string | null>(() => readNotice(location.state))
  const [submitted, setSubmitted] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  // Editor dibuat ulang saat data dari server berganti supaya isinya selalu sama dengan yang tersimpan
  const [reloads, setReloads] = useState(0)

  // Pesan sekali tampil dihapus dari state riwayat supaya tidak muncul lagi saat halaman dimuat ulang
  useEffect(() => {
    if (readNotice(location.state)) void navigate(`${location.pathname}${location.search}`, { replace: true, state: null })
  }, [location.pathname, location.search, location.state, navigate])

  const migration = query.data
  if (query.isPending) return <BoxSkeleton rows={8} />
  if (query.isError) return <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />
  if (!migration) return null

  const frozen = migration.state !== 'draft' || migration.first_execution_at !== null
  const readOnly = !canEdit || frozen
  const queryKey = migrationQuery(db, migration.id).queryKey

  const save = async (definition: Definition) => {
    const updated = await updateMigration(db, migration.id, definition, migration.revision)
    client.setQueryData(queryKey, updated)
    void client.invalidateQueries({ queryKey: ['databases', db, 'migrations'] })
    setNotice(`Draft disimpan sebagai revision ${updated.revision}.`)
  }

  const reload = async () => {
    await query.refetch()
    setReloads((count) => count + 1)
    setNotice(null)
  }

  const afterSubmit = async () => {
    await Promise.all([client.invalidateQueries({ queryKey }), client.invalidateQueries({ queryKey: ['databases', db, 'migrations'] }), client.invalidateQueries({ queryKey: ['databases', db, 'change-requests'] })])
    setSubmitted(true)
    setNotice(null)
  }

  const copy = () =>
    void navigate(newMigrationPath(db), { state: { prefill: { form: { ...formFromMigration(migration), title: `${migration.title} (salinan)` }, source: 'copy', origin: migration.title } } })

  return (
    <>
      <Breadcrumbs items={[{ label: 'Migration', to: migrationsPath(db) }, { label: migration.title }]} />
      <div className="d-flex flex-wrap align-items-start justify-content-between gap-2 mb-3">
        <div>
          <h2 className="h5 mb-1">{migration.title}</h2>
          <div className="d-flex flex-wrap align-items-center gap-2 small text-muted">
            <span className={`badge status${frozen ? ' status-pending' : ''}`}>{definitionStateLabel(migration.state)}</span>
            <span className="badge badge-soft">{modeLabel(migration.mode)}</span>
            {migration.irreversible && <span className="badge badge-soft">Irreversible</span>}
            <span>
              Revision <span className="mono">{migration.revision}</span>
            </span>
            <span title={migration.checksum}>
              Checksum <span className="mono">{migration.checksum.slice(0, 12)}</span>
            </span>
          </div>
        </div>
        {canEdit && frozen && (
          <button type="button" className="btn btn-sm btn-outline-secondary" onClick={copy}>
            <i className="fas fa-copy" aria-hidden="true" />
            Salin ke draft baru
          </button>
        )}
      </div>

      {notice && (
        <Notice kind="success" onDismiss={() => setNotice(null)}>
          {notice}
        </Notice>
      )}
      {submitted && (
        <Notice kind="success" onDismiss={() => setSubmitted(false)}>
          Permintaan review dibuat. Buka tab <Link to={reviewPath(db)}>Review</Link> untuk melihat statusnya.
        </Notice>
      )}
      {frozen && <Notice kind="info">{migration.first_execution_at ? 'Migration ini pernah dijalankan sehingga definisinya tidak bisa diubah.' : 'Definisi dibekukan karena sudah diajukan untuk review dan tidak bisa diubah lagi.'} Salin ke draft baru bila perlu mengubahnya.</Notice>}
      {!canEdit && <Notice kind="info">Anda hanya memiliki akses lihat pada database ini sehingga definisi tidak bisa diubah.</Notice>}

      <div className="card">
        <div className="card-body">
          <DefinitionEditor
            key={`${migration.state}:${migration.revision}:${reloads}`}
            initial={formFromMigration(migration)}
            readOnly={readOnly}
            submitLabel="Simpan draft"
            onSave={save}
            onReload={() => void reload()}
            onDirtyChange={setDirty}
            cancelTo={migrationsPath(db)}
            extraActions={
              <button type="button" className="btn btn-outline-primary" disabled={dirty} title={dirty ? 'Simpan perubahan terlebih dahulu' : undefined} onClick={() => setSubmitting(true)}>
                <i className="fas fa-paper-plane" aria-hidden="true" />
                Ajukan untuk review
              </button>
            }
          />
        </div>
      </div>

      {canEdit && <PlanPanel db={db} migration={migration} dirty={dirty} />}

      {submitting && <SubmitDialog db={db} migration={migration} onSubmitted={afterSubmit} onClose={() => setSubmitting(false)} />}
    </>
  )
}
