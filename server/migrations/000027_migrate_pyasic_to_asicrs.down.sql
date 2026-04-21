-- Revert asicrs driver name back to pyasic for devices that were migrated.
UPDATE discovered_device SET driver_name = 'pyasic' WHERE driver_name = 'asicrs';
