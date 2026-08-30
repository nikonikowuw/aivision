-- 000002_add_menu_parent_index.up.sql
CREATE INDEX IF NOT EXISTS idx_menus_parent_id ON menus (parent_id);
