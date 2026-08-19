-- 删除原有的单列唯一索引并创建带有软删除字段的复合唯一索引

-- Role
ALTER TABLE `roles` DROP INDEX `idx_roles_code`;
CREATE UNIQUE INDEX `uk_roles_code` ON `roles` (`code`, `deleted_at`);

-- User
ALTER TABLE `users` DROP INDEX `idx_users_username`;
CREATE UNIQUE INDEX `uk_users_username` ON `users` (`username`, `deleted_at`);
