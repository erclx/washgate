ALTER TABLE entitlement_changes DROP COLUMN IF EXISTS quota_reset_at;

DROP TABLE IF EXISTS quota_resets;
