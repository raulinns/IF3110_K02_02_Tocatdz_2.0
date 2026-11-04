import { PageHeader } from '../components/ui/PageHeader.tsx'
import { EmptyState } from '../components/ui/EmptyState.tsx'

interface PlaceholderProps {
  title: string
  task: string
  bare?: boolean
}

// Penanda halaman yang belum dikerjakan; diganti satu per satu oleh task FE-xx terkait.
export function Placeholder({ title, task, bare = false }: PlaceholderProps) {
  return (
    <>
      {!bare && <PageHeader pretitle="Segera hadir" title={title} />}
      <EmptyState icon="fa-hammer">
        Halaman {title} dikerjakan di {task}.
      </EmptyState>
    </>
  )
}
