-- Copying a prepaid wash into the two-plan table fails its check, which keeps the rollback from losing one.
CREATE TABLE washes_without_prepaid (
    id TEXT PRIMARY KEY,
    plate TEXT NOT NULL,
    company_id TEXT REFERENCES companies (id),
    plan TEXT NOT NULL CHECK (plan IN ('premium', 'fleet')),
    admitted_at TEXT NOT NULL
);

INSERT INTO washes_without_prepaid (id, plate, company_id, plan, admitted_at)
SELECT id, plate, company_id, plan, admitted_at FROM washes;

CREATE TEMP TABLE outbox_during_rebuild AS SELECT wash_id FROM outbox;
DELETE FROM outbox;
DROP TABLE washes;
ALTER TABLE washes_without_prepaid RENAME TO washes;
CREATE INDEX washes_plate_admitted_at ON washes (plate, admitted_at);
INSERT INTO outbox (wash_id) SELECT wash_id FROM outbox_during_rebuild;
DROP TABLE outbox_during_rebuild;

DROP TABLE prepaid_washes;
