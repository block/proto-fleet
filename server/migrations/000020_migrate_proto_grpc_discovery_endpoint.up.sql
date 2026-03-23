UPDATE discovered_device
SET url_scheme = 'https',
    port = '443',
    updated_at = NOW()
WHERE driver_name = 'proto'
  AND port = '2121'
  AND deleted_at IS NULL;
