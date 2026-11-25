import { useId } from 'react'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { SqlField } from '../../components/ui/SqlField.tsx'
import type { BatchForm, DefinitionForm, Problems } from '../../lib/definition.ts'

interface MigrationFormProps {
  value: DefinitionForm
  onChange: (next: DefinitionForm) => void
  problems: Problems
  readOnly: boolean
  // Dipanggil saat pengguna mengubah sebuah field supaya error server untuk field itu dihapus
  onEdited: (...fields: string[]) => void
}

const modes: { value: Schemas['ExecutionMode']; label: string; hint: string }[] = [
  { value: 'normal', label: 'Normal', hint: 'SQL lengkap dijalankan dalam satu transaction.' },
  { value: 'batch', label: 'Batch', hint: 'Pembaruan data bertahap pada satu tabel PostgreSQL. Isi bagian SET saja.' },
]

const batchFields: { key: keyof BatchForm; field: string; label: string; hint?: string; mono: boolean; type?: string }[] = [
  { key: 'schema', field: 'batch.schema', label: 'Schema', mono: true },
  { key: 'table', field: 'batch.table', label: 'Tabel', mono: true },
  { key: 'keyColumn', field: 'batch.key_column', label: 'Kolom kunci', hint: 'Unik, tidak null, dan tidak berubah (smallint, integer, atau bigint)', mono: true },
  { key: 'batchSize', field: 'batch.batch_size', label: 'Ukuran batch', hint: 'Jumlah baris per batch, 100 sampai 100000', mono: false, type: 'number' },
]

export function MigrationForm({ value, onChange, problems, readOnly, onEdited }: MigrationFormProps) {
  const prefix = useId()
  const batch = value.mode === 'batch'
  const first = (field: string) => problems[field]?.[0]
  const set = (patch: Partial<DefinitionForm>, ...fields: string[]) => {
    onChange({ ...value, ...patch })
    onEdited(...fields)
  }

  return (
    <>
      <Field id={`${prefix}-title`} label="Judul" error={first('title')}>
        {(control) => <input {...control} name="title" type="text" placeholder="Contoh: Tambah kolom telepon" value={value.title} readOnly={readOnly} onChange={(event) => set({ title: event.target.value }, 'title')} autoComplete="off" data-problem-field="title" />}
      </Field>

      <fieldset className="mb-3" data-field="mode" disabled={readOnly}>
        <legend className="form-label">Mode eksekusi</legend>
        <div className="d-flex flex-wrap gap-3">
          {modes.map((mode) => (
            <div className="form-check" key={mode.value}>
              <input
                className="form-check-input"
                type="radio"
                name={`${prefix}-mode`}
                id={`${prefix}-mode-${mode.value}`}
                value={mode.value}
                checked={value.mode === mode.value}
                aria-describedby={`${prefix}-mode-${mode.value}-hint`}
                onChange={() => set({ mode: mode.value }, 'mode', 'batch', 'up_sql', 'down_sql')}
              />
              <label className="form-check-label" htmlFor={`${prefix}-mode-${mode.value}`}>
                {mode.label}
              </label>
              <div id={`${prefix}-mode-${mode.value}-hint`} className="form-text mt-0">
                {mode.hint}
              </div>
            </div>
          ))}
        </div>
        {first('mode') && <div className="text-danger small mt-1">{first('mode')}</div>}
      </fieldset>

      {batch && (
        <fieldset className="mb-3" data-field="batch">
          <legend className="form-label">Konfigurasi batch</legend>
          {first('batch') && (
            <div className="text-danger small mb-2" role="alert">
              {first('batch')}
            </div>
          )}
          <div className="row g-3">
            {batchFields.map(({ key, field, label, hint, mono, type }) => (
              <div className="col-md-6" key={key}>
                <Field id={`${prefix}-${key}`} label={label} error={first(field)} hint={hint} className="mb-0">
                  {(control) => (
                    <input
                      {...control}
                      className={`${control.className}${mono ? ' mono' : ''}`}
                      name={key}
                      type={type ?? 'text'}
                      inputMode={type === 'number' ? 'numeric' : undefined}
                      value={value.batch[key]}
                      readOnly={readOnly}
                      onChange={(event) => set({ batch: { ...value.batch, [key]: event.target.value } }, field, 'batch')}
                      autoComplete="off"
                      spellCheck={false}
                      data-problem-field={field}
                    />
                  )}
                </Field>
              </div>
            ))}
            <div className="col-12">
              <Field id={`${prefix}-scope`} label="Predikat scope (opsional)" error={first('batch.scope_where')} hint="Ekspresi WHERE untuk membatasi baris. Saat ini server masih menolak predikat yang tidak kosong." className="mb-0">
                {(control) => (
                  <input
                    {...control}
                    className={`${control.className} mono`}
                    name="scope_where"
                    type="text"
                    value={value.batch.scopeWhere}
                    readOnly={readOnly}
                    onChange={(event) => set({ batch: { ...value.batch, scopeWhere: event.target.value } }, 'batch.scope_where', 'batch')}
                    autoComplete="off"
                    spellCheck={false}
                    data-problem-field="batch.scope_where"
                  />
                )}
              </Field>
            </div>
          </div>
        </fieldset>
      )}

      <div data-field="up_sql">
        <SqlField
          label={batch ? 'Bagian SET (up)' : 'SQL up'}
          value={value.upSql}
          onChange={(upSql) => set({ upSql }, 'up_sql')}
          readOnly={readOnly}
          errors={problems.up_sql}
          rows={batch ? 4 : 8}
          placeholder={batch ? "status = 'arsip'" : 'ALTER TABLE public.pelanggan ADD COLUMN telepon text;'}
          hint={batch ? "Tulis penugasan kolom tanpa kata SET atau UPDATE, misalnya status = 'arsip'." : 'Dijalankan dalam satu transaction. Jangan tulis BEGIN atau COMMIT sendiri.'}
        />
      </div>

      <div className="mb-3 form-check" data-field="irreversible">
        <input
          className="form-check-input"
          type="checkbox"
          id={`${prefix}-irreversible`}
          checked={value.irreversible}
          disabled={readOnly}
          aria-describedby={`${prefix}-irreversible-hint`}
          onChange={(event) => set({ irreversible: event.target.checked }, 'down_sql', 'irreversible_reason')}
        />
        <label className="form-check-label" htmlFor={`${prefix}-irreversible`}>
          Irreversible (tidak dapat dibatalkan)
        </label>
        <div id={`${prefix}-irreversible-hint`} className="form-text mt-0">
          Tandai bila perubahan ini tidak punya SQL down yang aman, misalnya menghapus data. Migration irreversible tidak bisa di-rollback.
        </div>
      </div>

      {value.irreversible ? (
        <Field id={`${prefix}-reason`} label="Alasan irreversible" error={first('irreversible_reason')} hint="Wajib diisi. Alasan ini ditampilkan kepada reviewer.">
          {(control) => (
            <textarea {...control} name="irreversible_reason" rows={3} value={value.reason} readOnly={readOnly} onChange={(event) => set({ reason: event.target.value }, 'irreversible_reason')} data-problem-field="irreversible_reason" />
          )}
        </Field>
      ) : (
        <div data-field="down_sql">
          <SqlField
            label={batch ? 'Bagian SET (down)' : 'SQL down'}
            value={value.downSql}
            onChange={(downSql) => set({ downSql }, 'down_sql')}
            readOnly={readOnly}
            errors={problems.down_sql}
            rows={batch ? 4 : 6}
            placeholder={batch ? "status = 'aktif'" : 'ALTER TABLE public.pelanggan DROP COLUMN telepon;'}
            hint="Dipakai saat rollback. Wajib diisi kecuali migration ditandai irreversible."
            className="mb-0"
          />
        </div>
      )}
    </>
  )
}
