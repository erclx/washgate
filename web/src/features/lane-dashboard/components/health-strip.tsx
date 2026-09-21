import type { SiteStatus } from '@/data-source/data-source'

import { formatAge } from '../format'

interface HealthStripProps {
  status: SiteStatus | null
  isAgentDown: boolean
  canSwitchLink: boolean
  onToggleLink: (isCut: boolean) => void
  now: number
}

function linkLabel(status: SiteStatus): { text: string; dotClass: string } {
  if (status.is_syncing) return { text: 'Syncing', dotClass: 'bg-accent' }
  if (status.link === 'online')
    return { text: 'Online', dotClass: 'bg-success' }
  return { text: 'Offline', dotClass: 'bg-warning' }
}

export function HealthStrip({
  status,
  isAgentDown,
  canSwitchLink,
  onToggleLink,
  now,
}: HealthStripProps) {
  const isCut = status?.link === 'cut'
  const label = status && linkLabel(status)
  return (
    <section
      aria-label="Site health"
      data-testid="health-strip"
      data-stale={isAgentDown}
      className="flex flex-wrap items-center gap-lg border-b border-border bg-surface px-lg py-sm data-[stale=true]:opacity-50"
    >
      {status && label && (
        <>
          <span className="flex items-center gap-sm text-body">
            <span
              aria-hidden="true"
              className={`size-2 rounded-full ${label.dotClass}`}
            />
            {label.text}
          </span>
          <span className="font-mono text-code">
            Outbox {status.outbox_depth}
          </span>
          {status.last_synced_at && (
            <span className="text-body text-muted">
              Synced {formatAge(now - Date.parse(status.last_synced_at))} ago
            </span>
          )}
        </>
      )}
      <button
        type="button"
        disabled={!canSwitchLink || status === null || isAgentDown}
        onClick={() => onToggleLink(!isCut)}
        className="ml-auto rounded-pill bg-background px-md py-xs text-body text-text hover:text-accent disabled:opacity-40"
      >
        {isCut ? 'Restore link' : 'Cut link'}
      </button>
    </section>
  )
}
