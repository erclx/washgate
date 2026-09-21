import type {
  LaneDecision,
  PlateLookup,
  SiteStatus,
  TraceStep,
} from '@/data-source/data-source'

export function buildTrace(
  overrides: Partial<Record<TraceStep['step'], TraceStep['status']>> = {},
): TraceStep[] {
  const steps: TraceStep['step'][] = [
    'entitlement_lookup',
    'ledger_write',
    'outbox_entry',
    'sync_to_hq',
  ]
  return steps.map((step) => ({
    step,
    status: overrides[step] ?? 'done',
    duration_ms: 3,
  }))
}

export function buildDecision(
  overrides: Partial<LaneDecision> = {},
): LaneDecision {
  return {
    id: 'dec-1',
    decided_at: '2026-09-21T12:29:58Z',
    plate: 'ABC123',
    confidence: 0.97,
    outcome: 'admit',
    reason: 'within_cap',
    wash_id: 'wash-1',
    wash_number: 5,
    company_name: null,
    cutoff: 0.8,
    has_photo: false,
    trace: buildTrace(),
    ...overrides,
  }
}

export function buildStaffDecision(
  overrides: Partial<LaneDecision> = {},
): LaneDecision {
  return buildDecision({
    id: 'dec-staff',
    plate: 'MLB48Z',
    confidence: 0.52,
    outcome: 'staff',
    reason: 'low_confidence',
    wash_id: null,
    wash_number: null,
    trace: buildTrace({
      entitlement_lookup: 'skipped',
      ledger_write: 'skipped',
      outbox_entry: 'skipped',
      sync_to_hq: 'skipped',
    }),
    ...overrides,
  })
}

export function buildStatus(overrides: Partial<SiteStatus> = {}): SiteStatus {
  return {
    site_id: 'site-1',
    link: 'online',
    outbox_depth: 0,
    last_synced_at: '2026-09-21T12:29:56Z',
    is_syncing: false,
    ...overrides,
  }
}

export function buildLookup(overrides: Partial<PlateLookup> = {}): PlateLookup {
  return {
    plate: 'ABC123',
    owner_type: 'customer',
    owner_name: 'demo@example.com',
    subscription: { plan: 'premium', status: 'active' },
    washes_this_month: 5,
    washes: [
      { admitted_at: '2026-09-12T08:10:00Z', site_id: 'site-1' },
      { admitted_at: '2026-09-09T16:40:00Z', site_id: 'site-1' },
    ],
    ...overrides,
  }
}
