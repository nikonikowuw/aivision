-- 000003_add_department_parent_index_and_fix_status_default.down.sql
ALTER TABLE departments ALTER COLUMN status SET DEFAULT 1;
DROP INDEX IF EXISTS idx_departments_parent_id;
