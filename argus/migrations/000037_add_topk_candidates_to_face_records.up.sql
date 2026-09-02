-- 000037_add_topk_candidates_to_face_records.up.sql
-- 为 face_observations 与 face_captures 添加 candidates_json 字段以保存 Top-K 候选匹配条目

ALTER TABLE face_observations ADD COLUMN candidates_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE face_captures ADD COLUMN best_candidates_json JSONB NOT NULL DEFAULT '[]'::jsonb;
