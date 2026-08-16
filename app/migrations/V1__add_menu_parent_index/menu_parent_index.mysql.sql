SET @index_exists := (
    SELECT COUNT(1)
    FROM information_schema.statistics
    WHERE table_schema = DATABASE()
      AND table_name = 'menus'
      AND index_name = 'idx_menus_parent_id'
);
SET @create_index := IF(
    @index_exists = 0,
    'CREATE INDEX idx_menus_parent_id ON menus (parent_id)',
    'SELECT 1'
);
PREPARE stmt FROM @create_index;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
