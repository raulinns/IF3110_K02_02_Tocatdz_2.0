import { act, screen } from '@testing-library/react'
import { EditorView } from '@codemirror/view'

// Editor CodeMirror tidak bisa diisi lewat user-event sehingga isinya diganti langsung lewat transaksi editor
async function viewOf(name: string | RegExp): Promise<EditorView> {
  const box = await screen.findByRole('textbox', { name })
  const view = EditorView.findFromDOM(box.closest('.cm-editor') as HTMLElement)
  if (!view) throw new Error('editor tidak ditemukan')
  return view
}

export async function setSql(name: string | RegExp, text: string) {
  const view = await viewOf(name)
  act(() => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } }))
}

export async function sqlOf(name: string | RegExp): Promise<string> {
  return (await viewOf(name)).state.doc.toString()
}
