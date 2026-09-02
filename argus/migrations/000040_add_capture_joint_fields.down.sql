-- 000040_add_capture_joint_fields.down.sql
DROP INDEX IF EXISTS idx_captures_sub_crop_image_id;
DROP INDEX IF EXISTS idx_captures_crop_image_id;
DROP INDEX IF EXISTS idx_captures_image_id;
DROP INDEX IF EXISTS idx_captures_track_id;
DROP INDEX IF EXISTS idx_captures_instance_id;

ALTER TABLE captures DROP COLUMN IF EXISTS time_synced;
ALTER TABLE captures DROP COLUMN IF EXISTS sub_bbox_json;
