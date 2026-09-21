-- The name the customer app shows for a customer, so the screen never carries an email.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS name VARCHAR(255) NOT NULL DEFAULT '';
