-- requires-shared: 1000

-- Keep fleet curtailment authority distinct from pool/configuration ownership.
ALTER TABLE device_write_attempt ADD COLUMN curtailment_command TEXT
 CHECK (curtailment_command IN ('Curtail','Uncurtail','ApplyCurtailmentConfig'));
