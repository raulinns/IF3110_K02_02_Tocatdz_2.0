export const defaultLanding = '/databases'

// Tujuan setelah login hanya boleh path di aplikasi ini dan tidak boleh kembali ke halaman login
export function safeRedirectPath(value: unknown): string {
  if (typeof value !== 'string' || value.length > 2048) return defaultLanding
  if (!value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return defaultLanding
  if (Array.from(value).some((char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127)) return defaultLanding
  const path = value.split(/[?#]/)[0]
  if (path === '/login' || path.startsWith('/login/')) return defaultLanding
  return value
}
