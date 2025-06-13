-- Update existing devices to have the correct miner type based on manufacturer
UPDATE device 
SET type = 'proto_miner' 
WHERE manufacturer = 'Block, Inc' OR manufacturer IS NULL; 