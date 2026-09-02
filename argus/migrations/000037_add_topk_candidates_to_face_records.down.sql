-- 000037_add_topk_candidates_to_face_records.down.sql
ALTER TABLE face_observations DROP COLUMN IF EXISTS candidates_json;
ALTER TABLE face_captures DROP COLUMN IF EXISTS best_candidates_json;
