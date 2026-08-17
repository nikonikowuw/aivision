CREATE INDEX IF NOT EXISTS idx_departments_parent_id ON departments (parent_id);

ALTER TABLE departments ALTER COLUMN status DROP DEFAULT;
