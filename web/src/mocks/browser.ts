import { setupWorker } from 'msw/browser'
import { handlers } from './handlers/index.ts'
import { apiPrefix } from './http.ts'
import { signInFromUrl } from './quick-login.ts'

const worker = setupWorker(...handlers)

export async function startMocking() {
  signInFromUrl(location.search)
  await worker.start({
    // Hanya request /api yang belum punya mock dianggap error dan request lain dilewatkan begitu saja
    onUnhandledFrame({ frame, defaults }) {
      const request = (frame.data as { request?: Request }).request
      if (request && new URL(request.url).pathname.startsWith(apiPrefix)) defaults.error()
    },
  })
}
