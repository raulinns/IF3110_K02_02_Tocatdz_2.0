const number = (value, fallback) => {
  if (value === undefined || value === '') return fallback
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

const list = (value) => (value ? String(value).split(',').map((item) => item.trim()).filter(Boolean) : [])

// Origin diambil dari alamat dasar supaya cek Origin dan CSRF di server lulus tanpa pengaturan tambahan
const originOf = (url) => (/^(https?:\/\/[^/]+)/.exec(url) || [])[1] || url

// Semua pengaturan datang dari variabel lingkungan dan diberi nilai bawaan yang cocok untuk satu mesin
export function loadConfig(env = {}) {
  const baseUrl = (env.BASE_URL || 'http://localhost:8080').replace(/\/+$/, '')
  const multipliers = list(env.MULTIPLIERS).map(Number).filter((value) => Number.isFinite(value) && value > 0)
  return {
    profile: env.PROFILE === 'smoke' ? 'smoke' : 'stress',
    baseUrl,
    origin: env.ORIGIN || originOf(baseUrl),
    password: env.K6_PASSWORD || 'tocatdz-demo-2026',
    editor: env.EDITOR_USER || 'editor',
    leaders: list(env.LEADER_USERS || 'reviewer,observer'),
    databases: list(env.DATABASES),
    dbCount: Math.max(1, number(env.DB_COUNT, 2)),
    sameDb: env.SAME_DB === 'true',
    readVus: Math.max(1, number(env.READ_VUS, 10)),
    jobVus: Math.max(1, number(env.JOB_VUS, 2)),
    multipliers: multipliers.length > 0 ? multipliers : [1, 2, 4, 8, 1],
    rampSeconds: Math.max(1, number(env.RAMP_SECONDS, 30)),
    holdSeconds: Math.max(1, number(env.HOLD_SECONDS, 60)),
    pollSeconds: Math.max(0.1, number(env.POLL_SECONDS, 1)),
    jobTimeoutSeconds: Math.max(5, number(env.JOB_TIMEOUT_SECONDS, 120)),
    submitRetries: Math.max(0, number(env.SUBMIT_RETRIES, 20)),
    readP95Ms: number(env.READ_P95_MS, 500),
    maxErrorRate: number(env.MAX_ERROR_RATE, 0.01),
  }
}
