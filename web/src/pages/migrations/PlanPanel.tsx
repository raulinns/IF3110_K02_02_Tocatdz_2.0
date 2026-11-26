import { useId, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { explainMigration } from '../../api/migrations.ts'
import { isStaleConflict } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { splitStatements } from '../../lib/sql.ts'
import { PlanView } from './PlanView.tsx'

const staleReasons = new Set(['revision_mismatch'])

interface PlanPanelProps {
  db: string
  migration: Schemas['Migration']
  // Isi editor yang belum disimpan membuat analisis tidak lagi sesuai dengan revision tersimpan
  dirty: boolean
}

const preview = (statement: string) => (statement.length > 70 ? `${statement.slice(0, 70).replace(/\s+/g, ' ')}…` : statement.replace(/\s+/g, ' '))

export function PlanPanel({ db, migration, dirty }: PlanPanelProps) {
  const prefix = useId()
  const [direction, setDirection] = useState<Schemas['Direction']>('up')
  const [statement, setStatement] = useState(1)
  const [analyze, setAnalyze] = useState(false)
  const mutation = useMutation({ mutationFn: () => explainMigration(db, migration.id, { revision: migration.revision, direction, statement, analyze }) })

  const sql = direction === 'down' ? migration.down_sql : migration.up_sql
  const statements = sql ? splitStatements(sql) : []
  const batch = migration.mode === 'batch'
  const error = mutation.error
  const statementProblem = fieldError(error, 'statement')
  const analyzeProblem = fieldError(error, 'analyze')
  const directionProblem = fieldError(error, 'direction')
  const general = error && !statementProblem && !analyzeProblem && !directionProblem ? errorMessage(error) : null
  const stale = isStaleConflict(error, staleReasons)

  function changeDirection(next: Schemas['Direction']) {
    setDirection(next)
    setStatement(1)
    mutation.reset()
  }

  return (
    <div className="card" aria-labelledby={`${prefix}-title`}>
      <div className="card-header">
        <i className="fas fa-diagram-project text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0" id={`${prefix}-title`}>
          EXPLAIN
        </h2>
      </div>
      <div className="card-body">
        <p className="text-muted small">
          Hasil menggambarkan kondisi database saat analisis, bukan jaminan plan saat migration dijalankan nanti. Analisis memakai revision <span className="mono">{migration.revision}</span> yang tersimpan dan tidak mengubah draft maupun riwayat.
        </p>
        {batch ? (
          <Notice kind="info">EXPLAIN hanya tersedia untuk migration mode normal.</Notice>
        ) : (
          <>
            {dirty && <Notice kind="warning">Ada perubahan yang belum disimpan. Simpan draft dulu karena analisis memakai revision yang tersimpan.</Notice>}
            {general && (
              <Notice kind="error">
                {general}
                {stale && ' Muat ulang halaman ini untuk melihat revisi terbaru.'}
              </Notice>
            )}
            <form
              onSubmit={(event) => {
                event.preventDefault()
                if (!mutation.isPending && !dirty && statements.length > 0) mutation.mutate()
              }}
              noValidate
            >
              <div className="row g-3 align-items-end">
                <div className="col-md-3">
                  <Field id={`${prefix}-direction`} label="Arah" error={directionProblem} className="mb-0">
                    {(control) => (
                      <select {...control} className={control.className.replace('form-control', 'form-select')} value={direction} onChange={(event) => changeDirection(event.target.value as Schemas['Direction'])}>
                        <option value="up">SQL up</option>
                        <option value="down" disabled={migration.irreversible}>
                          SQL down
                        </option>
                      </select>
                    )}
                  </Field>
                </div>
                <div className="col-md-5">
                  <Field id={`${prefix}-statement`} label="Statement" error={statementProblem} className="mb-0">
                    {(control) => (
                      <select
                        {...control}
                        className={control.className.replace('form-control', 'form-select')}
                        value={statement}
                        onChange={(event) => {
                          setStatement(Number(event.target.value))
                          mutation.reset()
                        }}
                        disabled={statements.length === 0}
                      >
                        {statements.length === 0 && <option value={1}>Tidak ada statement</option>}
                        {statements.map((text, index) => (
                          <option key={index} value={index + 1}>
                            {index + 1}. {preview(text)}
                          </option>
                        ))}
                      </select>
                    )}
                  </Field>
                </div>
                <div className="col-md-4">
                  <button type="submit" className="btn btn-primary w-100" disabled={mutation.isPending || dirty || statements.length === 0}>
                    {mutation.isPending ? (
                      <>
                        <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                        Menganalisis
                      </>
                    ) : (
                      <>
                        <i className="fas fa-magnifying-glass-chart" aria-hidden="true" />
                        Jalankan EXPLAIN
                      </>
                    )}
                  </button>
                </div>
              </div>
              <div className="form-check mt-3">
                <input className="form-check-input" type="checkbox" id={`${prefix}-analyze`} checked={analyze} onChange={(event) => setAnalyze(event.target.checked)} aria-describedby={`${prefix}-analyze-hint`} aria-invalid={analyzeProblem ? true : undefined} />
                <label className="form-check-label" htmlFor={`${prefix}-analyze`}>
                  Sertakan ANALYZE
                </label>
                <div id={`${prefix}-analyze-hint`} className="form-text mt-0">
                  ANALYZE menjalankan query sungguhan sehingga hanya diizinkan untuk SELECT yang tidak mengubah data.
                </div>
                {analyzeProblem && <div className="text-danger small">{analyzeProblem}</div>}
              </div>
            </form>
            {mutation.data && <PlanView plan={mutation.data} />}
          </>
        )}
      </div>
    </div>
  )
}
