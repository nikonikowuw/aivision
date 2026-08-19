-- MySQL requires altering the deleted_at column type and establishing a composite unique constraint that supports soft deletes correctly.
-- For MySQL, soft_delete plugin uses milliseconds since epoch (bigint).

ALTER TABLE users MODIFY COLUMN deleted_at bigint DEFAULT 0;
ALTER TABLE roles MODIFY COLUMN deleted_at bigint DEFAULT 0;

-- Drop the previous single column indexes or older partial indexes if they exist
-- Using DROP INDEX syntax for MySQL
SET @sqlStmt = IF(
    (SELECT COUNT(*) FROM information_schema.statistics WHERE table_name = 'users' AND index_name = 'uk_users_username' AND table_schema = DATABASE()) > 0,
    'DROP INDEX uk_users_username ON users',
    'SELECT 1'
);
PREPARE stmt FROM @sqlStmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sqlStmt = IF(
    (SELECT COUNT(*) FROM information_schema.statistics WHERE table_name = 'roles' AND index_name = 'uk_roles_code' AND table_schema = DATABASE()) > 0,
    'DROP INDEX uk_roles_code ON roles',
    'SELECT 1'
);
PREPARE stmt FROM @sqlStmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- Create composite unique indexes covering deleted_at and the unique field
CREATE UNIQUE INDEX uk_users_username ON users(username, deleted_at);
CREATE UNIQUE INDEX uk_roles_code ON roles(code, deleted_at);
