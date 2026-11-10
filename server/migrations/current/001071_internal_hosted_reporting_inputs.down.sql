LOCK TABLE hosted_reporting_authority,hosted_reporting_inputs,hosted_reporting_fact,hosted_reporting_fact_site IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_reporting_inputs) OR EXISTS(SELECT 1 FROM hosted_reporting_authority) THEN
  RAISE EXCEPTION 'preserve reporting inputs; restore a coordinated backup or fix forward';
 END IF;
END $$;
DROP TABLE hosted_reporting_fact_site;
DROP TABLE hosted_reporting_fact;
DROP TABLE hosted_reporting_inputs;
DROP TABLE hosted_reporting_authority;
