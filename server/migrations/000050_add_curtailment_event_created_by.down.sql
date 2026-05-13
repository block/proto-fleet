ALTER TABLE curtailment_event
    DROP CONSTRAINT IF EXISTS fk_curtailment_event_created_by,
    DROP COLUMN IF EXISTS created_by_user_id;
