-- 000003_add_department_parent_index_and_fix_status_default.up.sql
CREATE INDEX IF NOT EXISTS idx_departments_parent_id ON departments (parent_id);
ALTER TABLE departments ALTER COLUMN status DROP DEFAULT;
