-- Kept apart from vehicles, so a later plan change that rewrites the vehicle cannot erase a reset.
CREATE TABLE quota_resets (
    plate TEXT PRIMARY KEY,
    reset_at TEXT NOT NULL
);
