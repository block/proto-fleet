-- requires-shared: 1000
-- Module source time can be pinned by a stale PSU independently of fresh board
-- readings. Hosted history bounds collections before authorizing each source.
CREATE INDEX idx_container_module_metrics_module_collected_at
 ON container_module_metrics(module_id,collected_at DESC);
