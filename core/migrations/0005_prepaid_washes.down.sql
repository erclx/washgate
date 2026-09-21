ALTER TABLE entitlement_changes DROP CONSTRAINT IF EXISTS entitlement_changes_prepaid;
ALTER TABLE entitlement_changes DROP CONSTRAINT IF EXISTS entitlement_changes_kind;
ALTER TABLE entitlement_changes DROP COLUMN IF EXISTS prepaid_wash_id;
ALTER TABLE entitlement_changes DROP COLUMN IF EXISTS kind;

-- Restoring the two-value check fails while a prepaid wash is stored, which keeps the rollback from losing one.
ALTER TABLE washes DROP CONSTRAINT IF EXISTS washes_prepaid;
ALTER TABLE washes DROP CONSTRAINT IF EXISTS washes_plan;
ALTER TABLE washes ADD CONSTRAINT IF NOT EXISTS washes_plan CHECK (plan IN ('premium', 'fleet'));
ALTER TABLE washes DROP FOREIGN KEY IF EXISTS washes_prepaid_wash;
ALTER TABLE washes DROP COLUMN IF EXISTS prepaid_wash_id;

DROP TABLE IF EXISTS prepaid_washes;
