-- Demo data for a developer stack, applied by the compose seed service after migrate. Never run it against a test database.
-- It writes customers and prices only, so no entitlement changes, and every insert updates a row it finds in place, so a rerun changes nothing.

INSERT INTO customers (id, email, name, created_at) VALUES
    ('demo-anna', 'anna@demo.washgate.test', 'Anna Lindqvist', '2026-01-01 00:00:00'),
    ('demo-erik', 'erik@demo.washgate.test', 'Erik Johansson', '2026-01-01 00:00:00'),
    ('demo-sara', 'sara@demo.washgate.test', 'Sara Nilsson', '2026-01-01 00:00:00')
ON DUPLICATE KEY UPDATE email = VALUES(email), name = VALUES(name);

-- Amounts are in öre. Invoicing refuses a fleet wash with no fleet_wash price in force.
INSERT INTO prices (code, amount_ore, valid_from) VALUES
    ('premium', 29900, '2026-01-01 00:00:00'),
    ('single_wash', 19900, '2026-01-01 00:00:00'),
    ('fleet_wash', 14900, '2026-01-01 00:00:00')
ON DUPLICATE KEY UPDATE amount_ore = VALUES(amount_ore);
