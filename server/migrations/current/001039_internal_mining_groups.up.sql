-- requires-shared: 1000
LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO permission(key,description) VALUES ('miner:activate_mining_groups','Enable per-hashboard pool configuration on a container.') ON CONFLICT(key) DO NOTHING;
INSERT INTO role_permission(role_id,permission_id)
SELECT r.id,p.id FROM role r CROSS JOIN permission p
WHERE r.builtin_key='ADMIN' AND r.deleted_at IS NULL AND p.key='miner:activate_mining_groups'
ON CONFLICT(role_id,permission_id) DO NOTHING;

-- One journal per controller; desired assignments and controller observations
-- are deliberately separate. Every outbound request is committed before I/O.
CREATE TABLE container_mining_group_state (
 device_id BIGINT PRIMARY KEY REFERENCES device(id),
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 observed JSONB NOT NULL DEFAULT '{}',
 observed_at TIMESTAMPTZ,
 bindings JSONB NOT NULL DEFAULT '{}',
 operation_id UUID,
 attempt_id UUID REFERENCES device_write_attempt(attempt_id),
 actor_id BIGINT REFERENCES "user"(id),
 operation_kind TEXT NOT NULL DEFAULT '' CHECK(operation_kind IN ('','activate','pools')),
 status TEXT NOT NULL DEFAULT 'idle' CHECK(status IN ('idle','pending','applying','blocked','complete','failed')),
 message TEXT NOT NULL DEFAULT '',
 step JSONB,
 step_key UUID,
 controller_operation_id TEXT NOT NULL DEFAULT '',
 write_epoch BIGINT NOT NULL DEFAULT 0,
 revision BIGINT NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 retry_count INTEGER NOT NULL DEFAULT 0
   CONSTRAINT chk_container_mining_group_state_retry_count CHECK (retry_count BETWEEN 0 AND 7),
 next_reconcile_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 activation_resume_mining BOOLEAN,
 activation_stop_requested BOOLEAN NOT NULL DEFAULT FALSE,
 transfer_failure_count INTEGER NOT NULL DEFAULT 0
   CONSTRAINT chk_container_mining_group_state_transfer_failure_count CHECK (transfer_failure_count BETWEEN 0 AND 3),
 FOREIGN KEY(device_id,organization_id) REFERENCES device(id,org_id)
);
CREATE TABLE hashboard_pool_intent (
 organization_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL,
 device_id BIGINT NOT NULL REFERENCES container_mining_group_state(device_id),
 installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
 mac_address TEXT NOT NULL,
 -- Account is the pool's owner. Host-owned revisions have depth zero; customer
 -- dispatch remains disabled until authoritative board allocations exist.
 account_id BIGINT NOT NULL,
 pool_revision_id VARCHAR(36) NOT NULL,
 operation_id UUID NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(organization_id,hashboard_id),
 FOREIGN KEY(organization_id,hashboard_id) REFERENCES hashboard(org_id,id),
 FOREIGN KEY(organization_id,account_id,pool_revision_id) REFERENCES hosted_account_pool_revision(organization_id,account_id,revision_id)
);
CREATE INDEX idx_hashboard_pool_intent_device ON hashboard_pool_intent(device_id);
CREATE INDEX idx_container_mining_group_state_due
 ON container_mining_group_state(next_reconcile_at, device_id)
 WHERE status IN ('pending','applying','blocked');

-- Retain every request identity, including after a newer request completes.
CREATE TABLE mining_group_request (
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 request_id UUID NOT NULL,
 device_id BIGINT NOT NULL REFERENCES device(id),
 fingerprint TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(organization_id,request_id)
);
