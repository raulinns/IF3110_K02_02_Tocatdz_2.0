import { useEffect, useRef } from 'react'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { PostgreSQL, sql } from '@codemirror/lang-sql'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { Compartment, EditorState } from '@codemirror/state'
import { drawSelection, EditorView, highlightActiveLine, keymap, lineNumbers, placeholder as placeholderText } from '@codemirror/view'
import { tags } from '@lezer/highlight'

// Warna diambil dari variabel CSS sehingga editor ikut berganti saat tema gelap atau terang dipilih
const theme = EditorView.theme({
  '&': { color: 'var(--text)', backgroundColor: 'var(--field)', border: '1px solid var(--border-strong)', borderRadius: '3px', fontSize: '.8125rem' },
  '&.cm-focused': { outline: '2px solid var(--accent)', outlineOffset: '1px' },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.6', minHeight: 'var(--editor-min, 12rem)', maxHeight: '28rem', overflow: 'auto' },
  '.cm-content': { caretColor: 'var(--text)', padding: '.5rem 0' },
  '.cm-cursor': { borderLeftColor: 'var(--text)' },
  '.cm-gutters': { backgroundColor: 'var(--surface)', color: 'var(--text-muted)', border: 'none', borderRight: '1px solid var(--border)' },
  '.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--accent) 8%, transparent)' },
  '.cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--accent) 8%, transparent)' },
  '.cm-selectionBackground, &.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--accent) 30%, transparent)' },
  '.cm-placeholder': { color: 'var(--text-muted)' },
})

const highlight = syntaxHighlighting(
  HighlightStyle.define([
    { tag: tags.keyword, color: 'var(--sql-keyword)', fontWeight: '600' },
    { tag: [tags.string, tags.special(tags.string)], color: 'var(--sql-string)' },
    { tag: [tags.number, tags.bool, tags.null], color: 'var(--sql-number)' },
    { tag: [tags.comment, tags.lineComment, tags.blockComment], color: 'var(--sql-comment)', fontStyle: 'italic' },
    { tag: [tags.typeName, tags.standard(tags.name)], color: 'var(--sql-type)' },
  ]),
)

export interface SqlEditorProps {
  value: string
  onChange?: (value: string) => void
  readOnly?: boolean
  // id elemen yang menjadi label dan keterangan editor untuk pembaca layar
  labelledBy: string
  describedBy?: string
  invalid?: boolean
  placeholder?: string
}

const attributes = ({ labelledBy, describedBy, invalid, readOnly }: Pick<SqlEditorProps, 'labelledBy' | 'describedBy' | 'invalid' | 'readOnly'>) => {
  const values: Record<string, string> = { 'aria-labelledby': labelledBy, spellcheck: 'false', autocapitalize: 'none', autocorrect: 'off' }
  if (describedBy) values['aria-describedby'] = describedBy
  if (invalid) values['aria-invalid'] = 'true'
  if (readOnly) values['aria-readonly'] = 'true'
  return EditorView.contentAttributes.of(values)
}

// Editor SQL berbasis CodeMirror dengan teks sebagai satu-satunya sumber kebenaran di luar komponen
export default function SqlEditor({ value, onChange, readOnly = false, labelledBy, describedBy, invalid = false, placeholder }: SqlEditorProps) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const change = useRef(onChange)
  const flexible = useRef({ state: new Compartment(), attrs: new Compartment() })

  useEffect(() => {
    change.current = onChange
  })

  useEffect(() => {
    const { state, attrs } = flexible.current
    const editor = new EditorView({
      parent: host.current ?? undefined,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          history(),
          drawSelection(),
          highlightActiveLine(),
          EditorView.lineWrapping,
          keymap.of([...defaultKeymap, ...historyKeymap]),
          sql({ dialect: PostgreSQL }),
          highlight,
          theme,
          placeholder ? placeholderText(placeholder) : [],
          state.of([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
          attrs.of(attributes({ labelledBy, describedBy, invalid, readOnly })),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) change.current?.(update.state.doc.toString())
          }),
        ],
      }),
    })
    view.current = editor
    return () => {
      editor.destroy()
      view.current = null
    }
    // Editor dibuat sekali dan perubahan berikutnya dimasukkan lewat efek di bawah supaya riwayat undo tidak hilang
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const editor = view.current
    if (editor && editor.state.doc.toString() !== value) editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } })
  }, [value])

  useEffect(() => {
    const { state, attrs } = flexible.current
    view.current?.dispatch({
      effects: [state.reconfigure([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]), attrs.reconfigure(attributes({ labelledBy, describedBy, invalid, readOnly }))],
    })
  }, [readOnly, labelledBy, describedBy, invalid])

  return <div ref={host} className="sql-editor" />
}
