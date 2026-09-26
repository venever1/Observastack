ALTER TABLE tenants ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE tenants ADD COLUMN deleted_at TIMESTAMPTZ;

ALTER TABLE users ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE users ADD COLUMN deleted_at TIMESTAMPTZ;

ALTER TABLE tenant_members ADD CONSTRAINT chk_member_role CHECK (role IN ('admin', 'member'));

ALTER TABLE refresh_tokens ADD CONSTRAINT unique_token_hash UNIQUE (token_hash);
