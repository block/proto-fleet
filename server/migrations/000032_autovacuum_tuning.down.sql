-- Restore default autovacuum settings for tables modified in 000031

ALTER TABLE device_status RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor);
ALTER TABLE errors RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor);
