-- requires-shared: 1000
ALTER TABLE repair_ticket DROP CONSTRAINT ck_repair_ticket_category;
ALTER TABLE repair_ticket ADD CONSTRAINT ck_repair_ticket_category CHECK (category BETWEEN 1 AND 6);
-- Client tickets created without a component were stored as "Unspecified";
-- the component triage filter now uses "Unknown" for the same meaning.
UPDATE repair_ticket SET component = 'Unknown' WHERE component = 'Unspecified';
