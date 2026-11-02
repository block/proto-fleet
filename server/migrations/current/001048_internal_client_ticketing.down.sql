-- Keep the writer fence, refusal guard, and destructive DDL in one statement
-- so standalone execution cannot discard client context after a failed guard.
DO $$
BEGIN
 LOCK TABLE repair_ticket, repair_ticket_part, repair_ticket_comment,
  repair_ticket_target, repair_ticket_audience, container_module, hosted_account IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM repair_ticket WHERE category>2 OR description<>''
   OR created_by_account_id IS NOT NULL OR shared_with_clients
   OR resolution_note<>'' OR resolution_note_account_id IS NOT NULL)
  OR EXISTS (SELECT 1 FROM repair_ticket_target)
  OR EXISTS (SELECT 1 FROM repair_ticket_audience)
  OR EXISTS (SELECT 1 FROM repair_ticket_comment WHERE NOT internal OR account_id IS NOT NULL)
  OR EXISTS (SELECT 1 FROM container_module WHERE model<>'')
  OR EXISTS (SELECT 1 FROM repair_ticket_part GROUP BY ticket_id,inventory_part_id HAVING count(*)>1) THEN
  RAISE EXCEPTION 'client ticketing has durable data; preserve it and fix forward';
 END IF;

 ALTER TABLE repair_ticket DROP COLUMN resolution_note_account_id;
 ALTER TABLE repair_ticket DROP COLUMN resolution_note;
 DROP VIEW repair_ticket_asset_option;
 ALTER TABLE repair_ticket_comment DROP CONSTRAINT ck_ticket_comment_internal_scope;
 ALTER TABLE repair_ticket_comment DROP CONSTRAINT fk_ticket_comment_audience;
 ALTER TABLE repair_ticket_comment DROP COLUMN account_id;
 DROP INDEX idx_repair_ticket_comment_client_page;
 ALTER TABLE repair_ticket_comment DROP COLUMN internal;
 DROP TABLE repair_ticket_target;
 DROP TABLE repair_ticket_audience;
 ALTER TABLE repair_ticket DROP CONSTRAINT fk_repair_ticket_creator_account;
 ALTER TABLE repair_ticket DROP COLUMN shared_with_clients;
 ALTER TABLE repair_ticket DROP COLUMN created_by_account_id;
 ALTER TABLE repair_ticket DROP COLUMN description;
 ALTER TABLE repair_ticket DROP CONSTRAINT ck_repair_ticket_category;
 ALTER TABLE repair_ticket ADD CONSTRAINT ck_repair_ticket_category CHECK(category BETWEEN 1 AND 2);
 ALTER TABLE container_module DROP COLUMN model;
 ALTER TABLE hosted_account DROP COLUMN ticketing_enabled;

 DROP INDEX idx_repair_ticket_part_active_inventory_part;
 ALTER TABLE repair_ticket_part ADD CONSTRAINT uq_ticket_inventory_part UNIQUE(ticket_id,inventory_part_id);
END;
$$;
