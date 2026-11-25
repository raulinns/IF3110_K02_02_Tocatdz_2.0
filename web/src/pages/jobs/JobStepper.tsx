import type { Schemas } from '../../api/types.ts'
import { jobSteps, type StepState } from '../../lib/stepper.ts'

const icons: Record<StepState, string> = { done: 'fa-check', current: 'fa-circle-notch', upcoming: 'fa-circle', success: 'fa-circle-check', danger: 'fa-circle-xmark', warning: 'fa-triangle-exclamation' }

// Tahap saat ini ditandai dengan aria-current dan ikon, bukan hanya dengan warna
export function JobStepper({ status }: { status: Schemas['JobStatus'] }) {
  return (
    <ol className="stepper" aria-label="Tahapan job">
      {jobSteps(status).map((step) => (
        <li key={step.label} className={step.state === 'upcoming' ? '' : step.state} aria-current={step.state === 'current' || step.state === 'success' || step.state === 'danger' || step.state === 'warning' ? 'step' : undefined}>
          <i className={`fas ${icons[step.state]}${step.state === 'current' && status === 'running' ? ' fa-spin' : ''}`} aria-hidden="true" />
          {step.label}
        </li>
      ))}
    </ol>
  )
}
