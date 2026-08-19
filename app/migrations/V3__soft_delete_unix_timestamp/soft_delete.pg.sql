-- Postgres requires altering the deleted_at column type and establishing a composite unique constraint that supports soft deletes correctly.
-- For postgres, soft_delete plugin uses milliseconds since epoch (int8 / bigint).

ALTER TABLE users ALTER COLUMN deleted_at TYPE bigint USING COALESCE(deleted_at, 0);
ALTER TABLE users ALTER COLUMN deleted_at SET DEFAULT 0;

ALTER TABLE roles ALTER COLUMN deleted_at TYPE bigint USING COALESCE(deleted_at, 0);
ALTER TABLE roles ALTER COLUMN deleted_at SET DEFAULT 0;

-- Drop the previous single column indexes or older partial indexes if they exist (they might have been created by auto migration or previous script)
DROP INDEX IF EXISTS uk_users_username;
DROP INDEX IF EXISTS uk_roles_code;

-- Create composite unique indexes covering deleted_at and the unique field
CREATE UNIQUE INDEX uk_users_username ON users(username, deleted_at);
CREATE UNIQUE INDEX uk_roles_code ON roles(code, deleted_at);
