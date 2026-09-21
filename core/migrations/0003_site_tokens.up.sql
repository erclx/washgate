-- A site with no token hash cannot authenticate, so it cannot push washes or pull entitlements.
ALTER TABLE sites ADD COLUMN IF NOT EXISTS token_sha256 BINARY(32) NULL;

CREATE UNIQUE INDEX IF NOT EXISTS sites_token_sha256 ON sites (token_sha256);
