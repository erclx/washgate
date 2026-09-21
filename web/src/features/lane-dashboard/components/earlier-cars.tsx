import type { LaneDecision } from '@/data-source/data-source'

import { formatPlate, formatTime } from '../format'
import { OUTCOME_STYLES } from '../outcome-style'
import { reasonCopy } from '../reason-copy'

interface EarlierCarsProps {
  queued: readonly LaneDecision[]
  earlier: readonly LaneDecision[]
}

interface CarRowProps {
  decision: LaneDecision
  isQueued: boolean
}

function CarRow({ decision, isQueued }: CarRowProps) {
  const style = OUTCOME_STYLES[decision.outcome]
  const reason = reasonCopy(decision)
  return (
    <li className="grid grid-cols-[6rem_minmax(0,1fr)_auto] items-baseline gap-x-md gap-y-xs border-b border-border px-md py-sm last:border-b-0 sm:grid-cols-[6rem_4rem_minmax(0,1fr)_auto]">
      <span className="font-mono text-code">{formatPlate(decision.plate)}</span>
      <span className={`text-body ${style.textClass}`}>{style.word}</span>
      <span
        title={reason}
        className="col-span-3 row-start-2 truncate text-body text-text sm:col-span-1 sm:row-start-auto"
      >
        {reason}
      </span>
      <span className="text-right font-mono text-label text-muted">
        {isQueued ? 'Queued behind staff' : formatTime(decision.decided_at)}
      </span>
    </li>
  )
}

export function EarlierCars({ queued, earlier }: EarlierCarsProps) {
  if (queued.length === 0 && earlier.length === 0) return null
  return (
    <section
      aria-labelledby="earlier-cars-heading"
      className="rounded-card border border-border bg-surface"
    >
      <h2 id="earlier-cars-heading" className="sr-only">
        Earlier cars
      </h2>
      <ul>
        {[...queued].reverse().map((decision) => (
          <CarRow key={decision.id} decision={decision} isQueued />
        ))}
        {earlier.map((decision) => (
          <CarRow key={decision.id} decision={decision} isQueued={false} />
        ))}
      </ul>
    </section>
  )
}
