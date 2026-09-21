ALTER TABLE discovered_device
ADD COLUMN device_class INTEGER NOT NULL DEFAULT 0 CHECK (device_class IN (0, 1, 2));
