// Semua akses ditunda sampai dipakai karena modul stub hanya dimuat sekali sedangkan backend palsu dibuat ulang per test
export default {
  get CookieJar() {
    return globalThis.__k6.backend.http.CookieJar
  },
  expectedStatuses: (...codes) => globalThis.__k6.backend.http.expectedStatuses(...codes),
  request: (...args) => globalThis.__k6.backend.http.request(...args),
  setResponseCallback: () => undefined,
}
