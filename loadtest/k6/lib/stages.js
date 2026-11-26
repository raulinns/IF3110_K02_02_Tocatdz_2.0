// Nama tahap dipakai sebagai tag supaya hasil tiap tingkat beban bisa dipisahkan dari satu kali run
export function stageLabels(multipliers) {
  return multipliers.map((multiplier, index) => `tahap${index + 1}-x${multiplier}`)
}

// Beban naik ke tiap kelipatan lalu ditahan, dan kelipatan terakhir biasanya kembali ke normal untuk mengamati pemulihan
export function buildStages({ base, multipliers, rampSeconds, holdSeconds }) {
  return multipliers.flatMap((multiplier) => {
    const target = Math.max(1, Math.round(base * multiplier))
    return [
      { duration: `${rampSeconds}s`, target },
      { duration: `${holdSeconds}s`, target },
    ]
  })
}

export const totalSeconds = ({ multipliers, rampSeconds, holdSeconds }) => multipliers.length * (rampSeconds + holdSeconds)

// Tahap yang sedang berjalan menurut waktu sejak scenario dimulai, dan sesudah akhir tetap tahap terakhir
export function stageAt(config, elapsedSeconds) {
  const labels = stageLabels(config.multipliers)
  const span = config.rampSeconds + config.holdSeconds
  const index = Math.min(labels.length - 1, Math.max(0, Math.floor(elapsedSeconds / span)))
  return labels[index]
}

// Profil smoke hanya satu VU singkat untuk memastikan skrip dan akun benar sebelum beban sungguhan dijalankan
export function scenarioStages(config, base) {
  if (config.profile === 'smoke') return [{ duration: '20s', target: 1 }]
  return buildStages({ base, multipliers: config.multipliers, rampSeconds: config.rampSeconds, holdSeconds: config.holdSeconds })
}
