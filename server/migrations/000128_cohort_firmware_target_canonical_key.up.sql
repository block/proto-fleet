CREATE UNIQUE INDEX uq_cohort_firmware_target_canonical_type
    ON cohort_firmware_target (cohort_id, LOWER(BTRIM(manufacturer)), LOWER(BTRIM(model)));
