ALTER TABLE discovered_device
ADD COLUMN ip_address_inet inet GENERATED ALWAYS AS (NULLIF(ip_address, '')::inet) STORED;

CREATE INDEX idx_discovered_device_ip_inet_gist
ON discovered_device
USING gist (ip_address_inet inet_ops);
