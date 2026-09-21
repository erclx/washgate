import type { LaneDecision } from '@/data-source/data-source'

import { OUTCOME_STYLES } from '../outcome-style'
import { reasonCopy } from '../reason-copy'

interface DecisionCardProps {
  decision: LaneDecision
}

export function DecisionCard({ decision }: DecisionCardProps) {
  const style = OUTCOME_STYLES[decision.outcome]
  return (
    <div
      data-testid="decision-card"
      className={`rounded-pill p-md ${style.tintClass}`}
    >
      <p className={`font-display text-display ${style.textClass}`}>
        {style.word}
      </p>
      <p className="mt-xs text-body text-text">{reasonCopy(decision)}</p>
    </div>
  )
}
