-- requires-shared: 1000
-- Prospective application capture only. Mutable target summaries are not a
-- historical source and are deliberately not backfilled. No triggers are added.
CREATE TABLE hosted_curtailment_confirmation (
 id bigserial PRIMARY KEY,
 evidence_id uuid NOT NULL DEFAULT gen_random_uuid(),
 organization_id bigint NOT NULL,
 event_id bigint NOT NULL,
 device_id bigint NOT NULL,
 curtail_batch text NOT NULL,
 phase text NOT NULL CHECK (phase IN ('curtail','restore')),
 phase_batch text NOT NULL,
 sample_at timestamptz NOT NULL,
 confirmed_at timestamptz NOT NULL CHECK(confirmed_at>=sample_at),
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_curtailment_confirmation_evidence UNIQUE(evidence_id),
 CONSTRAINT uq_hosted_curtailment_confirmation_cycle UNIQUE(organization_id,event_id,device_id,curtail_batch,phase,phase_batch)
);
CREATE INDEX idx_hosted_curtailment_confirmation_restore
 ON hosted_curtailment_confirmation(organization_id,event_id,device_id,curtail_batch,confirmed_at)
 WHERE phase='restore';

CREATE INDEX idx_hosted_curtailment_confirmation_next
 ON hosted_curtailment_confirmation(organization_id,device_id,confirmed_at) WHERE phase='curtail';

-- Freeze the original customer cohort, including physical installation for
-- shared containers. Later allocations cannot acquire earlier observations.
CREATE TABLE hosted_curtailment_asset (
 confirmation_id bigint NOT NULL REFERENCES hosted_curtailment_confirmation(id),
 interval_id bigint NOT NULL REFERENCES hosted_account_asset_interval(id),
 installation_id bigint REFERENCES hashboard_installation(id),
 organization_id bigint NOT NULL,
 client_id bigint NOT NULL,
 subclient_id bigint,
 site_id bigint,
 kind integer NOT NULL CHECK(kind IN (5,7)),
 asset_id bigint NOT NULL,
 PRIMARY KEY(confirmation_id,interval_id)
);
CREATE INDEX idx_hosted_curtailment_asset_client
 ON hosted_curtailment_asset(organization_id,client_id,confirmation_id);
CREATE INDEX idx_hosted_curtailment_asset_subclient
 ON hosted_curtailment_asset(organization_id,subclient_id,confirmation_id) WHERE subclient_id IS NOT NULL;
