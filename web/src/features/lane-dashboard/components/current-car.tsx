import type { ReactNode } from 'react'

import type { LaneDecision } from '@/data-source/data-source'

import { formatPlate } from '../format'
import { formatScore } from '../reason-copy'
import { DecisionCard } from './decision-card'
import { PlateBadge } from './plate-badge'
import { TraceList } from './trace-list'

interface CurrentCarProps {
  decision: LaneDecision
  photoUrl: string | null
  staffPrompt: ReactNode
}

function isUnsureRead(decision: LaneDecision): boolean {
  return (
    decision.confidence < decision.cutoff ||
    decision.reason === 'malformed_plate'
  )
}

export function CurrentCar({
  decision,
  photoUrl,
  staffPrompt,
}: CurrentCarProps) {
  return (
    <article
      key={decision.id}
      aria-label="Current car"
      className="decision-enter grid grid-cols-1 gap-lg rounded-card border border-border bg-surface p-md md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_minmax(0,1fr)]"
    >
      {decision.has_photo && photoUrl ? (
        <img
          src={photoUrl}
          alt={`Lane photo of ${formatPlate(decision.plate)}`}
          className="aspect-[4/3] w-full rounded-pill object-cover"
        />
      ) : (
        <div className="flex aspect-[4/3] w-full items-center justify-center rounded-pill border border-dashed border-border text-label text-muted">
          No photo held
        </div>
      )}
      <div className="flex flex-col gap-sm">
        <p className="text-label text-muted">Plate read</p>
        <PlateBadge plate={decision.plate} isUnsure={isUnsureRead(decision)} />
        <p className="font-mono text-label text-muted">
          Confidence {formatScore(decision.confidence)}
        </p>
      </div>
      <DecisionCard decision={decision} />
      {decision.outcome === 'staff' ? (
        staffPrompt
      ) : (
        <TraceList trace={decision.trace} />
      )}
    </article>
  )
}
