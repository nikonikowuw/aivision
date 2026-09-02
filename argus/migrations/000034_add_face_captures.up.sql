-- 000034_add_face_captures.up.sql
-- 全量人脸抓拍与多快照时序记录主表：face_captures
-- 支持包含陌生人与已知人员的全量抓拍，单 Track 聚合保存 1~5 组快照

CREATE TABLE face_captures (
    id                     BIGSERIAL    PRIMARY KEY,
    event_id               VARCHAR(200) NOT NULL,
    instance_id            VARCHAR(64)  NOT NULL DEFAULT '',
    camera_id              VARCHAR(64)  NOT NULL,
    camera_name            VARCHAR(128) NOT NULL DEFAULT '',
    algorithm_id           VARCHAR(64)  NOT NULL DEFAULT '',
    algorithm_version      VARCHAR(32)  NOT NULL DEFAULT '',
    track_id               BIGINT       NOT NULL DEFAULT 0,
    best_similarity        REAL         NOT NULL DEFAULT 0,
    best_quality_score     REAL         NOT NULL DEFAULT 0,
    best_person_id         VARCHAR(64)  NOT NULL DEFAULT '',
    best_person_name       VARCHAR(128) NOT NULL DEFAULT '',
    best_image_id          VARCHAR(200) NOT NULL DEFAULT '',
    best_image_rel_path    VARCHAR(255) NOT NULL DEFAULT '',
    best_face_image_id     VARCHAR(200) NOT NULL DEFAULT '',
    best_face_rel_path     VARCHAR(255) NOT NULL DEFAULT '',
    best_bbox_json         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    snapshot_count         INTEGER      NOT NULL DEFAULT 1,
    snapshots_json         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    first_observed_at      TIMESTAMPTZ  NOT NULL,
    last_observed_at       TIMESTAMPTZ  NOT NULL,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at             BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_face_captures_event_id ON face_captures (event_id, deleted_at);
CREATE INDEX idx_face_captures_first_observed_at ON face_captures (first_observed_at DESC);
CREATE INDEX idx_face_captures_camera_id ON face_captures (camera_id);
CREATE INDEX idx_face_captures_best_person_id ON face_captures (best_person_id);
