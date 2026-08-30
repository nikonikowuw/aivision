-- 000021_add_alarm_records.up.sql
-- 告警记录模块主表：alarm_records
-- 遵循设计原则：单检测目标单记录（1 Target = 1 Record），包含目标图坐标与全景图相对路径
-- 遵循项目规范：显式 snake_case 列、无外键、毫秒软删除（deleted_at=0 表示活跃，唯一索引复合 deleted_at）

CREATE TABLE alarm_records (
    id                BIGSERIAL    PRIMARY KEY,
    event_id          VARCHAR(200) NOT NULL,
    instance_id       VARCHAR(36)  NOT NULL,
    camera_id         VARCHAR(36)  NOT NULL,
    algorithm_id      VARCHAR(64)  NOT NULL,
    algorithm_version VARCHAR(32)  NOT NULL,
    alarm_type_id     VARCHAR(128) NOT NULL,
    occurred_at       TIMESTAMPTZ  NOT NULL,
    time_synced       BOOLEAN      NOT NULL DEFAULT TRUE,
    target_label      VARCHAR(64)  NOT NULL DEFAULT '',
    confidence        REAL         NOT NULL DEFAULT 0,
    track_id          BIGINT       NOT NULL DEFAULT 0,
    bbox_json         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    image_id          VARCHAR(200) NOT NULL DEFAULT '',
    image_rel_path    VARCHAR(255) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_alarm_records_event_id ON alarm_records (event_id, deleted_at);
CREATE INDEX idx_alarm_records_occurred_at ON alarm_records (occurred_at DESC);
CREATE INDEX idx_alarm_records_camera_id ON alarm_records (camera_id);
CREATE INDEX idx_alarm_records_algorithm_id ON alarm_records (algorithm_id);
CREATE INDEX idx_alarm_records_image_id ON alarm_records (image_id);
CREATE INDEX idx_alarm_records_confidence ON alarm_records (confidence);
