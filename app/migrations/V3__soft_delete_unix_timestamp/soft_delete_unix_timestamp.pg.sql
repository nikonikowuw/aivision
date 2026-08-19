-- 1. 转换并修改 deleted_at 类型为 bigint (0 表示未删除)
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

-- 2. 重建 users 唯一索引为 (username, deleted_at)
DROP INDEX IF EXISTS users_username_key;
DROP INDEX IF EXISTS uk_users_username;
CREATE UNIQUE INDEX IF NOT EXISTS uk_users_username ON users (username, deleted_at);

-- 3. 重建 roles 唯一索引为 (code, deleted_at)
DROP INDEX IF EXISTS roles_code_key;
DROP INDEX IF EXISTS uk_roles_code;
CREATE UNIQUE INDEX IF NOT EXISTS uk_roles_code ON roles (code, deleted_at);
