-- Widen the per-miner rollout baseline beyond hashrate so the review gate can
-- show power, efficiency and temperature against each miner's own past.
ALTER TABLE firmware_rollout_device
    ADD COLUMN baseline_power_w DOUBLE PRECISION NULL,
    ADD COLUMN baseline_efficiency_jh DOUBLE PRECISION NULL,
    ADD COLUMN baseline_temp_c DOUBLE PRECISION NULL;
