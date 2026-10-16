-- requires-shared: 1000
-- Controller/site attribution and source ordering survive loss of physical placement.
CREATE TABLE hashboard_observation (
    hashboard_id BIGINT PRIMARY KEY REFERENCES hashboard(id),
    device_id BIGINT NOT NULL REFERENCES device(id),
    site_id BIGINT,
    last_seen_at TIMESTAMPTZ NOT NULL,
    source_at TIMESTAMPTZ,
    -- NULL allows asynchronous readings on initial discovery only.
    attribution_since TIMESTAMPTZ,
    released_at TIMESTAMPTZ
);

-- Seed existing hardware from its latest installation without scanning metric history.
INSERT INTO hashboard_observation(hashboard_id, device_id, site_id, last_seen_at, source_at, attribution_since, released_at)
SELECT i.hashboard_id, t.device_id, i.site_id, GREATEST(i.last_seen_at, i.removed_at), h.time,
    CASE WHEN EXISTS (SELECT 1 FROM hashboard_installation prior
        WHERE prior.hashboard_id=i.hashboard_id AND (prior.installed_at, prior.id)<(i.installed_at, i.id))
        THEN i.installed_at END,
    i.removed_at
FROM (SELECT DISTINCT ON (hashboard_id) * FROM hashboard_installation
      ORDER BY hashboard_id, installed_at DESC, id DESC) i
JOIN container_module m ON m.id=i.module_id
JOIN container_tank t ON t.id=m.tank_id
LEFT JOIN hashboard_latest_metrics h ON h.hashboard_id=i.hashboard_id;
