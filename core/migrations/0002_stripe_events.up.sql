-- Each Stripe event id is stored in the same transaction as its effect, so a redelivered event changes nothing.
CREATE TABLE IF NOT EXISTS stripe_events (
    event_id VARCHAR(255) NOT NULL PRIMARY KEY,
    type VARCHAR(255) NOT NULL,
    received_at DATETIME(6) NOT NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;
