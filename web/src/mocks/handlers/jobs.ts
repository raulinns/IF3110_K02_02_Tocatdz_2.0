import { http } from 'msw/http'
import type { Schemas } from '../../api/types.ts'
import { authorizedConnection, requireWorkflow } from '../access.ts'
import { badRequest, notFound, ok, pageOf, pagingKeys, readPaging, route, secured, sortBy, type Context } from '../http.ts'
import { tickJob } from '../jobs.ts'
import { toJob } from '../models.ts'
import { accessFor, getState, type MockJob } from '../state.ts'

const statuses: Schemas['JobStatus'][] = ['queued', 'running', 'succeeded', 'failed', 'partial', 'uncertain', 'partially_reverted']

// Job dicari tanpa database di path, dan job milik database yang tidak bisa diakses dianggap tidak ada supaya keberadaannya tidak bocor
function findJob({ params, user }: Context): MockJob {
  const job = getState().jobs.find((candidate) => candidate.id === params.job_id)
  if (!job || !accessFor(user.id, job.database_id)) throw notFound()
  return job
}

export const jobHandlers = [
  // Setiap pembacaan mengirim status saat ini lalu memajukan simulasi satu langkah untuk polling berikutnya
  http.get(
    route('/jobs/:job_id'),
    secured((ctx) => {
      const job = findJob(ctx)
      const response = ok(toJob(job))
      tickJob(job)
      return response
    }),
  ),

  http.get(
    route('/jobs/:job_id/attempts'),
    secured((ctx) => {
      const job = findJob(ctx)
      return ok(pageOf([...job.attempts].sort((a, b) => a.attempt - b.attempt), readPaging(ctx.url)))
    }, { query: pagingKeys }),
  ),

  http.get(
    route('/databases/:database_id/jobs'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      const status = ctx.url.searchParams.get('status')
      if (status !== null && !statuses.includes(status as Schemas['JobStatus'])) throw badRequest()
      const jobs = getState()
        .jobs.filter((job) => job.database_id === connection.id && (status === null || job.status === status))
        .sort(sortBy<MockJob>((job) => job.queued_at, (job) => job.id))
        .reverse()
      return ok(pageOf(jobs.map(toJob), readPaging(ctx.url, ['status'])))
    }, { query: [...pagingKeys, 'status'] }),
  ),
]
