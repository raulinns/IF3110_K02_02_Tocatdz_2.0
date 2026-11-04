import { Link } from 'react-router-dom'

const advice: Record<number, string> = {
  401: 'Sesi Anda sudah berakhir atau Anda belum masuk.',
  403: 'Anda tidak memiliki izin untuk membuka halaman atau menjalankan aksi ini.',
  404: 'Halaman atau data yang Anda cari tidak ditemukan.',
  429: 'Tunggu sebentar sebelum mencoba lagi.',
}

const titles: Record<number, string> = {
  401: 'Belum masuk',
  403: 'Akses ditolak',
  404: 'Tidak ditemukan',
  429: 'Terlalu banyak permintaan',
}

interface ErrorPageProps {
  status: number
  onRetry?: () => void
}

export function ErrorPage({ status, onRetry }: ErrorPageProps) {
  return (
    <section className="error-page">
      <p className="error-code">{status}</p>
      <h1 className="error-title">{titles[status] ?? 'Terjadi kesalahan'}</h1>
      <p className="error-advise">{advice[status] ?? 'Silakan coba lagi beberapa saat lagi.'}</p>
      {onRetry && (
        <button type="button" className="btn btn-outline-secondary btn-round me-2" onClick={onRetry}>
          <i className="fas fa-rotate-right" aria-hidden="true" />
          Coba lagi
        </button>
      )}
      {status === 401 ? (
        <Link className="btn btn-primary btn-round" to="/login">
          <i className="fas fa-right-to-bracket" aria-hidden="true" />
          Kembali ke halaman login
        </Link>
      ) : (
        <Link className="btn btn-primary btn-round" to="/databases">
          <i className="fas fa-database" aria-hidden="true" />
          Kembali ke daftar database
        </Link>
      )}
    </section>
  )
}
