-- requires-shared: 1000
ALTER TABLE mining_group_step_dispatch ADD COLUMN dispatch_token UUID NOT NULL DEFAULT gen_random_uuid();
