-- 000004_use_unix_millisecond_soft_delete.up.sql
-- 转换 soft delete 为 BIGINT (毫秒时间戳，0 表示未删除)，建立复合唯一索引

ALTER TABLE users 
    ALTER COLUMN deleted_at TYPE BIGINT USING (CASE WHEN deleted_at IS NULL THEN 0 ELSE (EXTRACT(EPOCH FROM deleted_at)*1000)::BIGINT END),
    ALTER COLUMN deleted_at SET DEFAULT 0,
    ALTER COLUMN deleted_at SET NOT NULL;

ALTER TABLE roles 
    ALTER COLUMN deleted_at TYPE BIGINT USING (CASE WHEN deleted_at IS NULL THEN 0 ELSE (EXTRACT(EPOCH FROM deleted_at)*1000)::BIGINT END),
    ALTER COLUMN deleted_at SET DEFAULT 0,
    ALTER COLUMN deleted_at SET NOT NULL;

ALTER TABLE menus 
    ALTER COLUMN deleted_at TYPE BIGINT USING (CASE WHEN deleted_at IS NULL THEN 0 ELSE (EXTRACT(EPOCH FROM deleted_at)*1000)::BIGINT END),
    ALTER COLUMN deleted_at SET DEFAULT 0,
    ALTER COLUMN deleted_at SET NOT NULL;

ALTER TABLE departments 
    ALTER COLUMN deleted_at TYPE BIGINT USING (CASE WHEN deleted_at IS NULL THEN 0 ELSE (EXTRACT(EPOCH FROM deleted_at)*1000)::BIGINT END),
    ALTER COLUMN deleted_at SET DEFAULT 0,
    ALTER COLUMN deleted_at SET NOT NULL;

DROP INDEX IF EXISTS idx_users_username;
CREATE UNIQUE INDEX IF NOT EXISTS uk_users_username ON users (username, deleted_at);

DROP INDEX IF EXISTS idx_roles_code;
CREATE UNIQUE INDEX IF NOT EXISTS uk_roles_code ON roles (code, deleted_at);
