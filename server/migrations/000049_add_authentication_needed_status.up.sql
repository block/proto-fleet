-- Add AUTHENTICATION_NEEDED to device_pairing.pairing_status ENUM

ALTER TABLE device_pairing 
MODIFY COLUMN pairing_status ENUM(
    'PENDING',
    'PAIRED', 
    'UNPAIRED',
    'FAILED',
    'AUTHENTICATION_NEEDED'
) NOT NULL;
