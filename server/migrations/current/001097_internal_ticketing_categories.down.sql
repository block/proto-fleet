-- Keep the writer fence, refusal guard, and constraint change in one statement
-- so standalone execution cannot drop Other tickets after a failed guard.
DO $$
BEGIN
 LOCK TABLE repair_ticket IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM repair_ticket WHERE category > 5) THEN
  RAISE EXCEPTION 'Other tickets exist; preserve them and fix forward';
 END IF;
 ALTER TABLE repair_ticket DROP CONSTRAINT ck_repair_ticket_category;
 ALTER TABLE repair_ticket ADD CONSTRAINT ck_repair_ticket_category CHECK (category BETWEEN 1 AND 5);
END;
$$;
