-- 000013_add_persons.up.sql
-- 人员基础信息表（人员管理 MVP）
-- 遵循项目规范：显式 snake_case 列、无外键、毫秒软删除（BaseModel）。
-- person_id 在包含软删除的所有记录中保持唯一。

CREATE TABLE persons (
    id         BIGSERIAL PRIMARY KEY,
    person_id  VARCHAR(64) NOT NULL UNIQUE,
    name       VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_persons_deleted_id ON persons (deleted_at, id);
CREATE INDEX idx_persons_name ON persons (deleted_at, name);
