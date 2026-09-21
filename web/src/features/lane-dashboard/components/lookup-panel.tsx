import type { PlateLookup, Site } from '@/data-source/data-source'

import { formatDay } from '../format'
import { PREMIUM_MONTHLY_CAP } from '../reason-copy'
import { PlateBadge } from './plate-badge'

export type LookupState =
  | { status: 'loading'; plate: string }
  | { status: 'found'; lookup: PlateLookup }
  | { status: 'none'; plate: string }
  | { status: 'error' }

interface LookupPanelProps {
  lookup: LookupState
  sites: Site[]
  onClose: () => void
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

function ownerLine(lookup: PlateLookup): string {
  if (lookup.owner_type === null) return 'No owner on record'
  if (lookup.owner_type === 'company') {
    return `Fleet car, billed to ${lookup.owner_name}`
  }
  if (lookup.subscription === null) return 'No subscription'
  return `${capitalize(lookup.subscription.plan)}, ${lookup.subscription.status}`
}

function FoundLookup({
  lookup,
  sites,
}: {
  lookup: PlateLookup
  sites: Site[]
}) {
  const siteName = (siteId: string) =>
    sites.find((site) => site.id === siteId)?.name ?? siteId
  const isPremium = lookup.subscription?.plan === 'premium'
  return (
    <>
      <PlateBadge plate={lookup.plate} isUnsure={false} />
      <p className="text-body">{ownerLine(lookup)}</p>
      <p className="font-mono text-code">
        {isPremium
          ? `${lookup.washes_this_month} of ${PREMIUM_MONTHLY_CAP} this month`
          : `${lookup.washes_this_month} this month`}
      </p>
      <h3 className="mt-sm text-label text-muted">Washes this month</h3>
      <ul className="flex flex-col gap-xs">
        {lookup.washes.map((wash) => (
          <li
            key={`${wash.admitted_at}-${wash.site_id}`}
            className="flex gap-md text-body"
          >
            <span className="font-mono text-code">
              {formatDay(wash.admitted_at)}
            </span>
            <span>{siteName(wash.site_id)}</span>
          </li>
        ))}
      </ul>
    </>
  )
}

export function LookupPanel({ lookup, sites, onClose }: LookupPanelProps) {
  return (
    <section
      aria-labelledby="lookup-heading"
      className="flex flex-col gap-sm rounded-card border border-border bg-surface p-md"
    >
      <div className="flex items-baseline justify-between">
        <h2 id="lookup-heading" className="font-display text-heading">
          Lookup
        </h2>
        <button
          type="button"
          onClick={onClose}
          className="text-label text-accent underline-offset-2 hover:underline"
        >
          Close
        </button>
      </div>
      {lookup.status === 'loading' && (
        <PlateBadge plate={lookup.plate} isUnsure={false} />
      )}
      {lookup.status === 'found' && (
        <FoundLookup lookup={lookup.lookup} sites={sites} />
      )}
      {lookup.status === 'none' && (
        <p className="text-body">No vehicle registered with {lookup.plate}.</p>
      )}
      {lookup.status === 'error' && (
        <p role="alert" className="text-body text-error">
          Can&apos;t reach head office.
        </p>
      )}
    </section>
  )
}
