export default {
  get vu() {
    return globalThis.__k6.execution.vu
  },
  get scenario() {
    return globalThis.__k6.execution.scenario
  },
}
