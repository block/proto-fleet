-- Adds Stratum protocol per pool so the server can distinguish SV1 from SV2
-- on every pool read. Existing rows default to 'sv1', which matches the
-- PoolProtocol.UNSPECIFIED -> SV1 semantics on the proto side (handlers
-- normalize UNSPECIFIED to 'sv1' on insert, so the DB never stores it).
--
-- The CHECK constraint keeps the value space closed — future protocols
-- (e.g. SV3) require a migration rather than a silent proto-level change.
ALTER TABLE pool
    ADD COLUMN protocol TEXT NOT NULL DEFAULT 'sv1'
    CHECK (protocol IN ('sv1', 'sv2'));
