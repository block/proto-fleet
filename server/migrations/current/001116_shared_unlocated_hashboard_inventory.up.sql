-- Keep accepted unlocated observations outside physical slot installations.
ALTER TABLE container_inventory ADD COLUMN unlocated_hashboards jsonb NOT NULL DEFAULT '[]'::jsonb;
