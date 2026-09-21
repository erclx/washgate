INSERT INTO companies (id, name) VALUES ('nordfrakt', 'Nordfrakt AB');

INSERT INTO vehicles (plate, plan, company_id) VALUES
    ('ABC123', 'premium', NULL),
    ('KLM456', 'fleet', 'nordfrakt'),
    ('KLM457', 'fleet', 'nordfrakt'),
    ('TAX123', 'premium', NULL),
    ('CLEAN', 'premium', NULL);
