-- 000011_add_cameras.up.sql
-- 摄像头视频源表（RTSP 管理 MVP）
-- 遵循项目规范：显式 snake_case 列、无外键、毫秒软删除（BaseModel）、UUID 由 Go 生成。

CREATE TABLE cameras (
    id                 BIGSERIAL PRIMARY KEY,
    camera_id          VARCHAR(36)  NOT NULL UNIQUE,           -- 不可变 UUID（Go 生成，用户不可编辑）
    protocol           VARCHAR(16)  NOT NULL DEFAULT 'rtsp',
    name               VARCHAR(128) NOT NULL,
    rtsp_url           VARCHAR(2048) NOT NULL,                 -- 完整 RTSP URL（可含百分号编码 userinfo）
    remark             VARCHAR(255) NOT NULL DEFAULT '',
    transport_policy   VARCHAR(16)  NOT NULL DEFAULT 'auto',
    config_hash        VARCHAR(64)  NOT NULL DEFAULT '',       -- 配置指纹（测活乐观并发控制）
    last_probe_status  VARCHAR(16)  NOT NULL DEFAULT 'never',  -- never|success|failed
    last_probe_at      TIMESTAMPTZ,
    last_probe_error_code   VARCHAR(64) NOT NULL DEFAULT '',
    last_probe_error_message VARCHAR(255) NOT NULL DEFAULT '',
    last_success_at    TIMESTAMPTZ,
    last_success_transport  VARCHAR(16) NOT NULL DEFAULT '',
    last_codec         VARCHAR(16)  NOT NULL DEFAULT '',
    last_width         INT          NOT NULL DEFAULT 0,
    last_height        INT          NOT NULL DEFAULT 0,
    last_fps           DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at         BIGINT NOT NULL DEFAULT 0               -- BaseModel 毫秒软删除
);

CREATE INDEX idx_cameras_deleted_id ON cameras (deleted_at, id);
CREATE INDEX idx_cameras_name ON cameras (deleted_at, name);
