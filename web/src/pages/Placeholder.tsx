import { PageHeader } from '../components/ui/PageHeader.tsx'
import { EmptyState } from '../components/ui/EmptyState.tsx'

interface PlaceholderProps {
  title: string
  bare?: boolean
}

// Penanda halaman yang belum tersedia.
export function Placeholder({ title, bare = false }: PlaceholderProps) {
  return (
    <>
      {!bare && <PageHeader pretitle="Platform" title={title} />}
      <EmptyState icon="fa-hammer">Halaman {title} belum tersedia.</EmptyState>
    </>
  )
}
