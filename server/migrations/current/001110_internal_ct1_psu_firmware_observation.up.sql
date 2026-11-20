-- requires-shared: 1074

-- A container's PSU firmware, observed like its hashboard firmware (001073):
-- the version every located board reports for its module's PSU in one
-- complete, fresh inventory, or that those versions differ. Neither set means
-- the PSU firmware version is unknown.
ALTER TABLE container_inventory
    ADD COLUMN psu_firmware_version text NOT NULL DEFAULT '',
    ADD COLUMN psu_firmware_mixed boolean NOT NULL DEFAULT false;
