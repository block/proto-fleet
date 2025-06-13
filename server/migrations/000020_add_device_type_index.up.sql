-- Create index for better query performance when filtering by type
CREATE INDEX idx_device_type ON device(type); 