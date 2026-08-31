-- 000023_add_motion_gate_to_instances.up.sql
ALTER TABLE algorithm_instances ADD COLUMN IF NOT EXISTS motion_gate_json JSONB NOT NULL DEFAULT '{}'::jsonb;
