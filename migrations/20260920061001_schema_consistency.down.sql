ALTER TABLE refresh_tokens DROP CONSTRAINT IF EXISTS unique_token_hash;
ALTER TABLE tenant_members DROP CONSTRAINT IF EXISTS chk_member_role;
ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE users DROP COLUMN IF EXISTS updated_at;
ALTER TABLE tenants DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE tenants DROP COLUMN IF EXISTS updated_at;
