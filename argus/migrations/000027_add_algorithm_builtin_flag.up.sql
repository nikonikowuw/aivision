-- 000027_add_algorithm_builtin_flag.up.sql
-- 为算法主表与算法版本表添加 is_builtin 系统内置标记

ALTER TABLE algorithms ADD COLUMN is_builtin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE algorithm_versions ADD COLUMN is_builtin BOOLEAN NOT NULL DEFAULT FALSE;
