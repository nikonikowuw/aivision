-- 000023_add_motion_gate_to_instances.down.sql
ALTER TABLE algorithm_instances DROP COLUMN IF EXISTS motion_gate_json;
