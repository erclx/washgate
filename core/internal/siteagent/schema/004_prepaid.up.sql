-- A prepaid wash is spent the moment spent_at is set, whether this site spent it or a pull reported another site did.
-- Nothing ever clears spent_at, so a late or repeated pull cannot give a spent wash back.
CREATE TABLE prepaid_washes (
    id TEXT PRIMARY KEY,
    plate TEXT NOT NULL,
    spent_at TEXT
);

CREATE INDEX prepaid_washes_plate_spent_at ON prepaid_washes (plate, spent_at);

-- SQLite cannot alter a check, so washes is rebuilt. The outbox points at washes, so its rows step
-- aside while the old table is dropped and return once the new one holds the same ids.
CREATE TABLE washes_with_prepaid (
    id TEXT PRIMARY KEY,
    plate TEXT NOT NULL,
    company_id TEXT REFERENCES companies (id),
    plan TEXT NOT NULL CHECK (plan IN ('premium', 'fleet', 'prepaid')),
    admitted_at TEXT NOT NULL,
    prepaid_wash_id TEXT,
    CHECK ((plan = 'prepaid') = (prepaid_wash_id IS NOT NULL))
);

INSERT INTO washes_with_prepaid (id, plate, company_id, plan, admitted_at)
SELECT id, plate, company_id, plan, admitted_at FROM washes;

CREATE TEMP TABLE outbox_during_rebuild AS SELECT wash_id FROM outbox;
DELETE FROM outbox;
DROP TABLE washes;
ALTER TABLE washes_with_prepaid RENAME TO washes;
CREATE INDEX washes_plate_admitted_at ON washes (plate, admitted_at);
INSERT INTO outbox (wash_id) SELECT wash_id FROM outbox_during_rebuild;
DROP TABLE outbox_during_rebuild;
