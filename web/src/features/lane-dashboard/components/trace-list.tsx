import type { TraceStep } from '@/data-source/data-source'

const STEP_LABELS: Record<TraceStep['step'], string> = {
  entitlement_lookup: 'Entitlement lookup',
  ledger_write: 'Ledger write',
  outbox_entry: 'Outbox entry',
  sync_to_hq: 'Sync to HQ',
}

interface TraceListProps {
  trace: TraceStep[]
}

function StepResult({ step }: { step: TraceStep }) {
  switch (step.status) {
    case 'done':
      return (
        <span className="font-mono text-code text-success">
          <span aria-hidden="true">✓</span>
          <span className="sr-only">Done</span>
          <span className="ml-sm text-label text-muted">
            {step.duration_ms} ms
          </span>
        </span>
      )
    case 'queued':
      return <span className="text-label text-warning">Queued</span>
    case 'skipped':
      return <span className="text-label text-muted">Skipped</span>
  }
}

export function TraceList({ trace }: TraceListProps) {
  return (
    <section aria-labelledby="trace-heading">
      <h2 id="trace-heading" className="text-label text-muted">
        Trace
      </h2>
      <ul className="mt-xs flex flex-col gap-xs">
        {trace.map((step) => (
          <li
            key={step.step}
            className="flex items-baseline justify-between gap-md"
          >
            <span className="text-body">{STEP_LABELS[step.step]}</span>
            <StepResult step={step} />
          </li>
        ))}
      </ul>
    </section>
  )
}
