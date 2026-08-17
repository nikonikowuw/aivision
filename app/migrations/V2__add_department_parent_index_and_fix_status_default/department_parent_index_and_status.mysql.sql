SET @index_exists := (
    SELECT COUNT(1)
    FROM information_schema.statistics
    WHERE table_schema = DATABASE()
      AND table_name = 'departments'
      AND index_name = 'idx_departments_parent_id'
);
SET @create_index := IF(
    @index_exists = 0,
    'CREATE INDEX idx_departments_parent_id ON departments (parent_id)',
    'SELECT 1'
);
PREPARE stmt FROM @create_index;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

ALTER TABLE departments ALTER COLUMN status DROP DEFAULT;
