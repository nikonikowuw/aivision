-- 000004_use_unix_millisecond_soft_delete.down.sql
DROP INDEX IF EXISTS uk_users_username;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (username);

DROP INDEX IF EXISTS uk_roles_code;
CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_code ON roles (code);

ALTER TABLE users 
    ALTER COLUMN deleted_at DROP NOT NULL,
    ALTER COLUMN deleted_at DROP DEFAULT,
    ALTER COLUMN deleted_at TYPE TIMESTAMPTZ USING (CASE WHEN deleted_at = 0 THEN NULL ELSE TO_TIMESTAMP(deleted_at / 1000.0) END);

ALTER TABLE roles 
    ALTER COLUMN deleted_at DROP NOT NULL,
    ALTER COLUMN deleted_at DROP DEFAULT,
    ALTER COLUMN deleted_at TYPE TIMESTAMPTZ USING (CASE WHEN deleted_at = 0 THEN NULL ELSE TO_TIMESTAMP(deleted_at / 1000.0) END);

ALTER TABLE menus 
    ALTER COLUMN deleted_at DROP NOT NULL,
    ALTER COLUMN deleted_at DROP DEFAULT,
    ALTER COLUMN deleted_at TYPE TIMESTAMPTZ USING (CASE WHEN deleted_at = 0 THEN NULL ELSE TO_TIMESTAMP(deleted_at / 1000.0) END);

ALTER TABLE departments 
    ALTER COLUMN deleted_at DROP NOT NULL,
    ALTER COLUMN deleted_at DROP DEFAULT,
    ALTER COLUMN deleted_at TYPE TIMESTAMPTZ USING (CASE WHEN deleted_at = 0 THEN NULL ELSE TO_TIMESTAMP(deleted_at / 1000.0) END);
