-- 000007_add_system_configs.up.sql
-- 创建通用的系统配置表，支撑对时、网络、存储等系统级配置

CREATE TABLE IF NOT EXISTS system_configs (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    key VARCHAR(64) NOT NULL,
    value JSONB NOT NULL DEFAULT '{}'::jsonb,
    remark VARCHAR(255)
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_system_configs_key ON system_configs (key);

-- 初始化对时服务的默认配置 (key: 'system:time')
INSERT INTO system_configs (key, value, remark)
VALUES (
    'system:time',
    '{"mode": "ntp", "servers": ["pool.ntp.org", "ntp.aliyun.com"]}'::jsonb,
    '系统对时配置'
)
ON CONFLICT (key) DO NOTHING;
