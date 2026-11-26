import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { loadConfig } from '../lib/config.js'
import { buildStages, scenarioStages, stageAt, stageLabels, totalSeconds } from '../lib/stages.js'
import { buildThresholds } from '../lib/thresholds.js'

const config = loadConfig({ RAMP_SECONDS: '30', HOLD_SECONDS: '60' })

describe('tahapan beban', () => {
  it('naik ke tiap kelipatan lalu menahannya dan kembali ke normal di akhir', () => {
    const stages = buildStages({ base: 10, multipliers: [1, 2, 4, 8, 1], rampSeconds: 30, holdSeconds: 60 })
    assert.deepEqual(stages.map((stage) => stage.target), [10, 10, 20, 20, 40, 40, 80, 80, 10, 10])
    assert.deepEqual(stages.slice(0, 2).map((stage) => stage.duration), ['30s', '60s'])
    assert.equal(stages.length, 10)
  })

  it('jumlah VU tidak pernah di bawah satu walau pembulatan menghasilkan nol', () => {
    const stages = buildStages({ base: 1, multipliers: [0.1], rampSeconds: 1, holdSeconds: 1 })
    assert.equal(stages[0].target, 1)
  })

  it('profil smoke hanya satu VU selama dua puluh detik', () => {
    assert.deepEqual(scenarioStages(loadConfig({ PROFILE: 'smoke' }), 50), [{ duration: '20s', target: 1 }])
    assert.equal(scenarioStages(config, 10).length, 10)
  })

  it('total waktu cocok dengan jumlah durasi seluruh tahap', () => {
    assert.equal(totalSeconds(config), 450)
    const seconds = scenarioStages(config, 10).reduce((sum, stage) => sum + Number.parseInt(stage.duration, 10), 0)
    assert.equal(seconds, 450)
  })
})

describe('label tahap', () => {
  it('memberi nama unik berurutan menurut kelipatannya', () => {
    assert.deepEqual(stageLabels([1, 2, 4, 8, 1]), ['tahap1-x1', 'tahap2-x2', 'tahap3-x4', 'tahap4-x8', 'tahap5-x1'])
  })

  it('stageAt memilih tahap menurut waktu dan batasnya', () => {
    assert.equal(stageAt(config, 0), 'tahap1-x1')
    assert.equal(stageAt(config, 89), 'tahap1-x1')
    assert.equal(stageAt(config, 90), 'tahap2-x2')
    assert.equal(stageAt(config, 359), 'tahap4-x8')
    assert.equal(stageAt(config, 360), 'tahap5-x1')
  })

  it('di luar rentang tetap memakai tahap pertama atau terakhir', () => {
    assert.equal(stageAt(config, -5), 'tahap1-x1')
    assert.equal(stageAt(config, 99999), 'tahap5-x1')
  })
})

describe('ambang batas', () => {
  const labels = stageLabels(config.multipliers)
  const thresholds = buildThresholds(config, labels)

  it('hanya tiga ambang yang bisa menggagalkan run dan sisanya kosong', () => {
    const active = Object.entries(thresholds).filter(([, rules]) => rules.length > 0)
    assert.deepEqual(active, [
      ['http_req_duration{kind:read}', ['p(95)<500']],
      ['http_req_failed', ['rate<0.01']],
      ['job_success', ['rate>0.95']],
    ])
  })

  it('setiap tahap mendapat hasil terpisah untuk latensi baca, error, throughput, dan metrik job', () => {
    for (const label of labels) {
      for (const key of [`http_req_duration{kind:read,stage:${label}}`, `http_req_failed{stage:${label}}`, `http_reqs{stage:${label}}`, `job_queue_wait_ms{stage:${label}}`, `job_duration_ms{stage:${label}}`, `jobs_completed{stage:${label}}`]) {
        assert.ok(key in thresholds, `${key} tidak ada`)
      }
    }
  })

  it('angka ambang mengikuti konfigurasi', () => {
    const custom = buildThresholds(loadConfig({ READ_P95_MS: '250', MAX_ERROR_RATE: '0.05' }), [])
    assert.deepEqual(custom['http_req_duration{kind:read}'], ['p(95)<250'])
    assert.deepEqual(custom.http_req_failed, ['rate<0.05'])
  })
})
