-- 000035_seed_face_capture_menu.down.sql
DELETE FROM role_menus WHERE menu_id IN (
    SELECT id FROM menus WHERE permission LIKE 'record:capture%' OR name = 'RecordCapture'
);
DELETE FROM menus WHERE parent_id IN (
    SELECT id FROM menus WHERE name = 'RecordCapture'
);
DELETE FROM menus WHERE name = 'RecordCapture';
