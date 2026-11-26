export const check = (value, rules) => {
  const results = Object.entries(rules).map(([name, rule]) => [name, rule(value)])
  globalThis.__k6.checks.push(...results)
  return results.every(([, ok]) => ok)
}

export const fail = (message) => {
  throw new Error(message)
}

export const sleep = (seconds) => globalThis.__k6.backend.sleep(seconds)
