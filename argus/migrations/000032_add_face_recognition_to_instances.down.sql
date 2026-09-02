-- 000032_add_face_recognition_to_instances.down.sql
DROP TABLE IF EXISTS face_gallery_revision CASCADE;
ALTER TABLE algorithm_instances DROP COLUMN IF EXISTS face_recognition_json;
