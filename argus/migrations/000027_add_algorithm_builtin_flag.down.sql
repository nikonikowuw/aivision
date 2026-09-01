-- 000027_add_algorithm_builtin_flag.down.sql
ALTER TABLE algorithms DROP COLUMN is_builtin;
ALTER TABLE algorithm_versions DROP COLUMN is_builtin;
