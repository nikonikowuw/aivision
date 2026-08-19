-- 删除原有的单列唯一索引并创建带有软删除字段的复合唯一索引
-- Postgres 在 gorm.DeletedAt（使用 NULL）的情况下，由于 NULL 视为不同，因此复合索引对 NULL 值仍然允许重复。
-- 但是我们需要确保当 deleted_at IS NULL 时，username 是唯一的。

-- Role
DROP INDEX IF EXISTS idx_roles_code;
CREATE UNIQUE INDEX uk_roles_code ON roles(code) WHERE deleted_at IS NULL;

-- User
DROP INDEX IF EXISTS idx_users_username;
CREATE UNIQUE INDEX uk_users_username ON users(username) WHERE deleted_at IS NULL;
