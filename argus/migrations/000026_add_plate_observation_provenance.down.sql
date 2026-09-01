-- 000026_add_plate_observation_provenance.down.sql
DROP INDEX IF EXISTS idx_plate_observations_plate_image_id;
DROP INDEX IF EXISTS idx_plate_observations_image_id;
DROP INDEX IF EXISTS idx_plate_observations_algorithm_id;

ALTER TABLE plate_observations
    DROP COLUMN IF EXISTS plate_image_rel_path,
    DROP COLUMN IF EXISTS plate_image_id,
    DROP COLUMN IF EXISTS image_rel_path,
    DROP COLUMN IF EXISTS image_id,
    DROP COLUMN IF EXISTS time_synced,
    DROP COLUMN IF EXISTS algorithm_version,
    DROP COLUMN IF EXISTS algorithm_id;
