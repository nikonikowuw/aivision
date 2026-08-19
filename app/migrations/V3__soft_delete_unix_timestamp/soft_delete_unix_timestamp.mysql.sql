-- 1. 转换历史数据 deleted_at 并在软删除表中改类型为 bigint unsigned / bigint
-- users 表
ALTER TABLE users MODIFY COLUMN deleted_at BIGINT UNSIGNED NOT NULL DEFAULT 0;
-- roles 表
ALTER TABLE roles MODIFY COLUMN deleted_at BIGINT UNSIGNED NOT NULL DEFAULT 0;
-- menus 表
ALTER TABLE menus MODIFY COLUMN deleted_at BIGINT UNSIGNED NOT NULL DEFAULT 0;
-- departments 表
ALTER TABLE departments MODIFY COLUMN deleted_at BIGINT UNSIGNED NOT NULL DEFAULT 0;

-- 2. 重建 users 唯一索引为 (username, deleted_at)
SET @u_idx_exists := (
    SELECT COUNT(1) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'users' AND index_name = 'username'
);
SET @drop_u_idx := IF(@u_idx_exists > 0, 'DROP INDEX username ON users', 'SELECT 1');
PREPARE stmt FROM @drop_u_idx; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @u_idx_exists2 := (
    SELECT COUNT(1) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'users' AND index_name = 'uk_users_username'
);
SET @drop_u_idx2 := IF(@u_idx_exists2 > 0, 'DROP INDEX uk_users_username ON users', 'SELECT 1');
PREPARE stmt FROM @drop_u_idx2; EXECUTE stmt; DEALLOCATE PREPARE stmt;

CREATE UNIQUE INDEX uk_users_username ON users (username, deleted_at);

-- 3. 重建 roles 唯一索引为 (code, deleted_at)
SET @r_idx_exists := (
    SELECT COUNT(1) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'roles' AND index_name = 'code'
);
SET @drop_r_idx := IF(@r_idx_exists > 0, 'DROP INDEX code ON roles', 'SELECT 1');
PREPARE stmt FROM @drop_r_idx; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @r_idx_exists2 := (
    SELECT COUNT(1) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'roles' AND index_name = 'uk_roles_code'
);
SET @drop_r_idx2 := IF(@r_idx_exists2 > 0, 'DROP INDEX uk_roles_code ON roles', 'SELECT 1');
PREPARE stmt FROM @drop_r_idx2; EXECUTE stmt; DEALLOCATE PREPARE stmt;

CREATE UNIQUE INDEX uk_roles_code ON roles (code, deleted_at);
