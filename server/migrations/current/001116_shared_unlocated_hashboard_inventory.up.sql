-- Keep accepted unlocated observations outside physical slot installations.
ALTER TABLE container_inventory ADD COLUMN unlocated_hashboards jsonb NOT NULL DEFAULT '[]'::jsonb;

-- A site change withdraws evidence from the previous site. The existing module
-- trigger also advances observed_at to reject delayed observations.
CREATE FUNCTION withdraw_unlocated_hashboard_site_readings() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    UPDATE container_inventory SET unlocated_hashboards = '[]'::jsonb WHERE device_id = NEW.id;
    RETURN NEW;
END;
$$;

CREATE TRIGGER unlocated_hashboard_site_readings AFTER UPDATE OF site_id ON device
FOR EACH ROW WHEN (OLD.site_id IS DISTINCT FROM NEW.site_id)
EXECUTE FUNCTION withdraw_unlocated_hashboard_site_readings();
