-- 000040_add_capture_joint_fields.up.sql
-- 为已创建的 captures 表补齐联合抓拍字段及模型声明的查询索引。
-- 000038 保持初始建表契约不变；SQLite 运行时由 GORM AutoMigrate 补齐同样字段。

ALTER TABLE captures ADD COLUMN sub_bbox_json JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE captures ADD COLUMN time_synced BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_captures_instance_id ON captures (instance_id);
CREATE INDEX IF NOT EXISTS idx_captures_track_id ON captures (track_id);
CREATE INDEX IF NOT EXISTS idx_captures_image_id ON captures (image_id);
CREATE INDEX IF NOT EXISTS idx_captures_crop_image_id ON captures (crop_image_id);
CREATE INDEX IF NOT EXISTS idx_captures_sub_crop_image_id ON captures (sub_crop_image_id);
