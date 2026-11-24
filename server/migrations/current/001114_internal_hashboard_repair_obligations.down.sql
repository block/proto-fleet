LOCK TABLE repair_ticket_board_owner,hashboard_repair_obligation,hashboard_repair_obligation_ticket IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hashboard_repair_obligation) OR EXISTS(SELECT 1 FROM repair_ticket_board_owner) THEN
  RAISE EXCEPTION 'hashboard repair obligations contain retained history; preserve it and fix forward';
 END IF;
END $$;
DROP TABLE hashboard_repair_obligation_ticket;
DROP TABLE hashboard_repair_obligation;
DROP TABLE repair_ticket_board_owner;
