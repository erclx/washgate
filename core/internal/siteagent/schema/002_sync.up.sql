CREATE TABLE outbox (
    wash_id TEXT PRIMARY KEY REFERENCES washes (id)
);

CREATE TABLE sync_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    cursor INTEGER NOT NULL,
    last_pulled_at TEXT
);

INSERT INTO sync_state (id, cursor, last_pulled_at) VALUES (1, 0, NULL);
