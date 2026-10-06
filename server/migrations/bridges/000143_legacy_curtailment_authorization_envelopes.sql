-- Compatibility bridge for databases created before migration 000143.
--
-- Migration 000143 deliberately rejects any pre-contract response profiles or
-- events. That assumption did not hold on real v0.2.x deployments. This bridge
-- reaches the same final schema without changing the shipped migration:
-- existing rows keep their executable scope, while their authorization envelope
-- is conservatively marked organization-wide. The new authorization checks
-- therefore require live organization-wide permission before those rows can be
-- reused; the bridge never infers narrower historical grants from current
-- topology.
--
-- The application runs this only while schema_migrations is locked at version
-- 142/clean or 143/dirty and both envelope columns are absent.

ALTER TABLE curtailment_response_profile
    ADD COLUMN authorization_envelope_jsonb JSONB;

ALTER TABLE curtailment_event
    ADD COLUMN authorization_envelope_jsonb JSONB;

UPDATE curtailment_response_profile
SET authorization_envelope_jsonb = jsonb_build_object(
    'schema_version', 1,
    'selected_resource_site_ids', '[]'::jsonb,
    'current_member_site_ids', '[]'::jsonb,
    'miner_scope_unbounded', true,
    'facility_fan_site_ids', '[]'::jsonb,
    -- Profiles did not persist a fan-site authorization snapshot before this
    -- contract. Any profile containing fans must require organization-wide site
    -- read permission until it is saved through the new API.
    'facility_fan_scope_unbounded', cardinality(facility_fan_device_ids) > 0
);

UPDATE curtailment_event
SET authorization_envelope_jsonb = jsonb_build_object(
    'schema_version', 1,
    'selected_resource_site_ids', '[]'::jsonb,
    'current_member_site_ids', '[]'::jsonb,
    'miner_scope_unbounded', true,
    -- Events already carry the fan-site snapshot captured when they started.
    'facility_fan_site_ids', to_jsonb(facility_fan_site_ids),
    'facility_fan_scope_unbounded', false
);

ALTER TABLE curtailment_response_profile
    ALTER COLUMN authorization_envelope_jsonb SET NOT NULL,
    ADD CONSTRAINT curtailment_response_profile_authorization_envelope_shape CHECK (
        jsonb_typeof(authorization_envelope_jsonb) = 'object'
        AND authorization_envelope_jsonb ?& ARRAY[
            'schema_version',
            'selected_resource_site_ids',
            'current_member_site_ids',
            'miner_scope_unbounded',
            'facility_fan_site_ids',
            'facility_fan_scope_unbounded'
        ]
        AND jsonb_typeof(authorization_envelope_jsonb->'schema_version') = 'number'
        AND authorization_envelope_jsonb->>'schema_version' = '1'
        AND jsonb_typeof(authorization_envelope_jsonb->'selected_resource_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'current_member_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'miner_scope_unbounded') = 'boolean'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_scope_unbounded') = 'boolean'
    );

ALTER TABLE curtailment_event
    ALTER COLUMN authorization_envelope_jsonb SET NOT NULL,
    ADD CONSTRAINT curtailment_event_authorization_envelope_shape CHECK (
        jsonb_typeof(authorization_envelope_jsonb) = 'object'
        AND authorization_envelope_jsonb ?& ARRAY[
            'schema_version',
            'selected_resource_site_ids',
            'current_member_site_ids',
            'miner_scope_unbounded',
            'facility_fan_site_ids',
            'facility_fan_scope_unbounded'
        ]
        AND jsonb_typeof(authorization_envelope_jsonb->'schema_version') = 'number'
        AND authorization_envelope_jsonb->>'schema_version' = '1'
        AND jsonb_typeof(authorization_envelope_jsonb->'selected_resource_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'current_member_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'miner_scope_unbounded') = 'boolean'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_scope_unbounded') = 'boolean'
    );

-- The bridge has applied the exact schema owned by migration 000143. Mark that
-- immutable migration complete so golang-migrate starts at the next version.
UPDATE schema_migrations
SET version = 143,
    dirty = false
WHERE (version = 142 AND NOT dirty)
   OR (version = 143 AND dirty);
