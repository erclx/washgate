export type Outcome = 'admit' | 'pay' | 'staff'

export type Reason =
  | 'within_cap'
  | 'fleet'
  | 'duplicate_read'
  | 'cap_reached'
  | 'prepaid_wash'
  | 'unknown_plate'
  | 'unknown_plate_offline'
  | 'low_confidence'
  | 'malformed_plate'
  | 'confirmed_by_staff'
  | 'sent_to_pay_by_staff'

export type TraceStepName =
  'entitlement_lookup' | 'ledger_write' | 'outbox_entry' | 'sync_to_hq'

export type TraceStepStatus = 'done' | 'skipped' | 'queued'

export interface TraceStep {
  step: TraceStepName
  status: TraceStepStatus
  duration_ms: number
}

export interface LaneDecision {
  id: string
  decided_at: string
  plate: string
  confidence: number
  outcome: Outcome
  reason: Reason
  wash_id: string | null
  wash_number: number | null
  company_name: string | null
  cutoff: number
  has_photo: boolean
  trace: TraceStep[]
}

export interface SyncedBody {
  wash_ids: string[]
}

export type LaneEvent =
  | { type: 'decision'; body: LaneDecision }
  | { type: 'resolved'; body: LaneDecision }
  | { type: 'synced'; body: SyncedBody }

export type LinkState = 'online' | 'offline' | 'cut'

export interface SiteStatus {
  site_id: string
  link: LinkState
  outbox_depth: number
  last_synced_at: string | null
  is_syncing: boolean
}

export interface Site {
  id: string
  name: string
}

export interface PlateWash {
  admitted_at: string
  site_id: string
}

export interface PlateLookup {
  plate: string
  owner_type: 'customer' | 'company' | null
  owner_name: string | null
  subscription: { plan: string; status: string } | null
  washes_this_month: number
  washes: PlateWash[]
}

export type LookupResult =
  { isFound: true; lookup: PlateLookup } | { isFound: false; plate: string }

export type StaffAction = 'confirm' | 'send_to_pay'

export type Unsubscribe = () => void

export interface LaneData {
  subscribeToLane(
    siteId: string,
    onEvent: (event: LaneEvent) => void,
    onUnreachable: () => void,
  ): Unsubscribe
  photoUrl(siteId: string, decisionId: string): string | null
  confirmPlate(siteId: string, decisionId: string, plate: string): Promise<void>
  sendToPay(siteId: string, decisionId: string): Promise<void>
  enabledStaffActions(decisionId: string): StaffAction[]
}

export interface SiteData {
  sites(): Site[]
  subscribeToStatus(
    siteId: string,
    onStatus: (status: SiteStatus) => void,
    onUnreachable: () => void,
  ): Unsubscribe
  setLinkCut(siteId: string, isCut: boolean): Promise<void>
  canSwitchLink(): boolean
}

export interface LookupData {
  lookUpPlate(plate: string): Promise<LookupResult>
}

export interface DataSource {
  kind: 'live' | 'replay'
  lane: LaneData
  site: SiteData
  lookup: LookupData
}

export class UnreachableError extends Error {
  constructor(target: string, options?: ErrorOptions) {
    super(`Could not reach ${target}`, options)
    this.name = 'UnreachableError'
  }
}
