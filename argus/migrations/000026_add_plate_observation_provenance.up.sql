-- 000026_add_plate_observation_provenance.up.sql
ALTER TABLE plate_observations
    ADD COLUMN algorithm_id VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN algorithm_version VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN time_synced BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN image_id VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN image_rel_path VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN plate_image_id VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN plate_image_rel_path VARCHAR(255) NOT NULL DEFAULT '';

CREATE INDEX idx_plate_observations_algorithm_id
    ON plate_observations (algorithm_id);
CREATE INDEX idx_plate_observations_image_id
    ON plate_observations (image_id);
CREATE INDEX idx_plate_observations_plate_image_id
    ON plate_observations (plate_image_id);
