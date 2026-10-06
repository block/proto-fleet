-- Current evidence only. Retain the source watermark when evidence disappears.
ALTER TABLE container_module ADD COLUMN issues jsonb,
 ADD COLUMN issues_observed_at timestamptz,
 ADD COLUMN issues_source_times jsonb NOT NULL DEFAULT '{}'::jsonb;
