import { vi } from 'vitest'

interface Behavior {
  delay?: number
  failures?: number
}

// Memperlambat atau menggagalkan sejumlah request tertentu tanpa mengubah request lainnya
export function interceptFetch(match: string, { delay = 0, failures = 0 }: Behavior = {}) {
  const original = globalThis.fetch
  let remaining = failures
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    if (String(input).includes(match)) {
      if (delay > 0) await new Promise((resolve) => setTimeout(resolve, delay))
      if (remaining > 0) {
        remaining -= 1
        throw new TypeError('Failed to fetch')
      }
    }
    return original(input, init)
  })
}

export const callsTo = (spy: { mock: { calls: unknown[][] } }, match: string) => spy.mock.calls.filter((call) => String(call[0]).includes(match)).length
