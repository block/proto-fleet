-- requires-shared: 1067
-- Keep the original source and physical selection after retries and later requests.
CREATE TABLE mining_group_pool_handoff (
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 operation_id UUID NOT NULL,
 device_id BIGINT NOT NULL,
 captured JSONB NOT NULL,
 completed_at TIMESTAMPTZ,
 PRIMARY KEY (organization_id, operation_id),
 FOREIGN KEY (device_id, organization_id) REFERENCES device(id, org_id),
 FOREIGN KEY (organization_id, operation_id) REFERENCES mining_group_request(organization_id, request_id)
);
