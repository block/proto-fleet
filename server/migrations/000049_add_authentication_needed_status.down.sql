-- Remove AUTHENTICATION_NEEDED from device_pairing.pairing_status ENUM

-- First, update any AUTHENTICATION_NEEDED records to PENDING
UPDATE device_pairing 
SET pairing_status = 'PENDING' 
WHERE pairing_status = 'AUTHENTICATION_NEEDED';

-- Then remove the enum value
ALTER TABLE device_pairing 
MODIFY COLUMN pairing_status ENUM(
    'PENDING',
    'PAIRED', 
    'UNPAIRED',
    'FAILED'
) NOT NULL;
