const perStage = ['job_queue_wait_ms', 'job_duration_ms', 'job_e2e_ms', 'submit_wait_ms', 'submit_conflicts', 'jobs_completed', 'jobs_failed', 'job_success']

// Ambang yang sebenarnya hanya tiga, dan ambang kosong per tahap dibuat supaya k6 memisahkan hasil tiap tingkat beban di ringkasan
// Ambang suatu scenario dihilangkan bila scenario itu tidak dijalankan karena metrik tanpa sampel dianggap nol dan menggagalkan run
export function buildThresholds(config, labels) {
  const reads = config.scenarios.includes('reads')
  const jobs = config.scenarios.includes('jobs')
  const thresholds = {}
  if (reads) thresholds['http_req_duration{kind:read}'] = [`p(95)<${config.readP95Ms}`]
  thresholds.http_req_failed = [`rate<${config.maxErrorRate}`]
  if (jobs) thresholds.job_success = ['rate>0.95']
  for (const label of labels) {
    if (reads) thresholds[`http_req_duration{kind:read,stage:${label}}`] = []
    thresholds[`http_req_failed{stage:${label}}`] = []
    thresholds[`http_reqs{stage:${label}}`] = []
    if (jobs) for (const metric of perStage) thresholds[`${metric}{stage:${label}}`] = []
  }
  return thresholds
}
