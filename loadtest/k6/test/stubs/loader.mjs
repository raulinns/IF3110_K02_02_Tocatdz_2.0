const stubs = {
  k6: 'k6.js',
  'k6/execution': 'execution.js',
  'k6/http': 'http.js',
  'k6/metrics': 'metrics.js',
}

// Modul k6 diganti stub supaya stress.js bisa dijalankan di Node tanpa biner k6
export async function resolve(specifier, context, nextResolve) {
  if (specifier in stubs) return { url: new URL(`./${stubs[specifier]}`, import.meta.url).href, shortCircuit: true }
  return nextResolve(specifier, context)
}
