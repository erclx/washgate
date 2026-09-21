-- A reset makes every site count a plate's monthly cap from reset_at. Its id is stored with its effect, so a retried reset changes nothing.
CREATE TABLE IF NOT EXISTS quota_resets (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    plate VARCHAR(16) NOT NULL,
    reset_at DATETIME(6) NOT NULL,
    note VARCHAR(255) NULL,
    INDEX quota_resets_plate_reset_at (plate, reset_at),
    CONSTRAINT quota_resets_vehicle FOREIGN KEY (plate) REFERENCES vehicles (plate)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

ALTER TABLE entitlement_changes ADD COLUMN IF NOT EXISTS quota_reset_at DATETIME(6) NULL;
