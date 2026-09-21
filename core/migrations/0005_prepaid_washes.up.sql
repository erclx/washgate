-- One paid single wash, keyed on the Checkout session that paid it, so a repeated event grants it once.
-- A spend only ever fills spent_wash_id, so no replay can give a spent wash back.
CREATE TABLE IF NOT EXISTS prepaid_washes (
    id VARCHAR(255) NOT NULL PRIMARY KEY,
    plate VARCHAR(16) NOT NULL,
    customer_id VARCHAR(64) NOT NULL,
    paid_at DATETIME(6) NOT NULL,
    spent_wash_id VARCHAR(64) NULL,
    spent_at DATETIME(6) NULL,
    INDEX prepaid_washes_plate_spent (plate, spent_wash_id),
    CONSTRAINT prepaid_washes_vehicle FOREIGN KEY (plate) REFERENCES vehicles (plate),
    CONSTRAINT prepaid_washes_customer FOREIGN KEY (customer_id) REFERENCES customers (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

ALTER TABLE washes ADD COLUMN IF NOT EXISTS prepaid_wash_id VARCHAR(255) NULL;
ALTER TABLE washes ADD CONSTRAINT washes_prepaid_wash FOREIGN KEY IF NOT EXISTS (prepaid_wash_id) REFERENCES prepaid_washes (id);
ALTER TABLE washes DROP CONSTRAINT IF EXISTS washes_plan;
ALTER TABLE washes ADD CONSTRAINT IF NOT EXISTS washes_plan CHECK (plan IN ('premium', 'fleet', 'prepaid'));
ALTER TABLE washes ADD CONSTRAINT IF NOT EXISTS washes_prepaid CHECK ((plan = 'prepaid') = (prepaid_wash_id IS NOT NULL));

-- A plan change names no prepaid wash, and a prepaid change names exactly one.
ALTER TABLE entitlement_changes ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'plan';
ALTER TABLE entitlement_changes ADD COLUMN IF NOT EXISTS prepaid_wash_id VARCHAR(255) NULL;
ALTER TABLE entitlement_changes ADD CONSTRAINT IF NOT EXISTS entitlement_changes_kind CHECK (kind IN ('plan', 'prepaid_granted', 'prepaid_spent'));
ALTER TABLE entitlement_changes ADD CONSTRAINT IF NOT EXISTS entitlement_changes_prepaid CHECK ((kind = 'plan') = (prepaid_wash_id IS NULL));
