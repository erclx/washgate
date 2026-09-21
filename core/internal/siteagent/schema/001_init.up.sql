CREATE TABLE companies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE vehicles (
    plate TEXT PRIMARY KEY,
    plan TEXT NOT NULL CHECK (plan IN ('premium', 'fleet')),
    company_id TEXT REFERENCES companies (id),
    CHECK ((plan = 'fleet') = (company_id IS NOT NULL))
);

CREATE TABLE washes (
    id TEXT PRIMARY KEY,
    plate TEXT NOT NULL,
    company_id TEXT REFERENCES companies (id),
    plan TEXT NOT NULL CHECK (plan IN ('premium', 'fleet')),
    admitted_at TEXT NOT NULL
);

CREATE INDEX washes_plate_admitted_at ON washes (plate, admitted_at);
