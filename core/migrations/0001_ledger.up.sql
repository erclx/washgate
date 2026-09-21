CREATE TABLE IF NOT EXISTS sites (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    last_synced_at DATETIME(6) NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS customers (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at DATETIME(6) NOT NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS companies (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS vehicles (
    plate VARCHAR(16) NOT NULL PRIMARY KEY,
    customer_id VARCHAR(64) NULL,
    company_id VARCHAR(64) NULL,
    leasing_company VARCHAR(255) NULL,
    CONSTRAINT vehicles_customer FOREIGN KEY (customer_id) REFERENCES customers (id),
    CONSTRAINT vehicles_company FOREIGN KEY (company_id) REFERENCES companies (id),
    CONSTRAINT vehicles_one_owner CHECK (customer_id IS NULL OR company_id IS NULL)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS subscriptions (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    plate VARCHAR(16) NOT NULL,
    plan VARCHAR(16) NOT NULL,
    customer_id VARCHAR(64) NULL,
    company_id VARCHAR(64) NULL,
    status VARCHAR(16) NOT NULL,
    current_period_end DATETIME(6) NULL,
    stripe_subscription_id VARCHAR(255) NULL UNIQUE,
    INDEX subscriptions_plate_status (plate, status),
    CONSTRAINT subscriptions_vehicle FOREIGN KEY (plate) REFERENCES vehicles (plate),
    CONSTRAINT subscriptions_customer FOREIGN KEY (customer_id) REFERENCES customers (id),
    CONSTRAINT subscriptions_company FOREIGN KEY (company_id) REFERENCES companies (id),
    CONSTRAINT subscriptions_plan CHECK (plan IN ('premium', 'fleet')),
    CONSTRAINT subscriptions_status CHECK (status IN ('active', 'canceled')),
    CONSTRAINT subscriptions_owner CHECK ((plan = 'fleet') = (company_id IS NOT NULL))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS prices (
    code VARCHAR(32) NOT NULL,
    amount_ore INT UNSIGNED NOT NULL,
    valid_from DATETIME(6) NOT NULL,
    PRIMARY KEY (code, valid_from)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS washes (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    site_id VARCHAR(64) NOT NULL,
    plate VARCHAR(16) NOT NULL,
    plan VARCHAR(16) NOT NULL,
    company_id VARCHAR(64) NULL,
    admitted_at DATETIME(6) NOT NULL,
    received_at DATETIME(6) NOT NULL,
    INDEX washes_plate_admitted_at (plate, admitted_at),
    INDEX washes_company_admitted_at (company_id, admitted_at),
    CONSTRAINT washes_site FOREIGN KEY (site_id) REFERENCES sites (id),
    CONSTRAINT washes_company FOREIGN KEY (company_id) REFERENCES companies (id),
    CONSTRAINT washes_plan CHECK (plan IN ('premium', 'fleet'))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS entitlement_changes (
    seq BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    plate VARCHAR(16) NOT NULL,
    plan VARCHAR(16) NULL,
    company_id VARCHAR(64) NULL,
    company_name VARCHAR(255) NULL,
    changed_at DATETIME(6) NOT NULL,
    CONSTRAINT entitlement_changes_company FOREIGN KEY (company_id) REFERENCES companies (id),
    CONSTRAINT entitlement_changes_plan CHECK (plan IN ('premium', 'fleet')),
    CONSTRAINT entitlement_changes_owner CHECK ((plan <=> 'fleet') = (company_id IS NOT NULL))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

-- Every entitlement write locks this one row first, so changes commit in seq order and a pull never skips one.
CREATE TABLE IF NOT EXISTS entitlement_cursor (
    id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
    CONSTRAINT entitlement_cursor_single_row CHECK (id = 1)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

INSERT INTO entitlement_cursor (id) VALUES (1) ON DUPLICATE KEY UPDATE id = id;
