-- Migrate devices discovered by the deprecated pyasic plugin to the asicrs plugin.
UPDATE discovered_device SET driver_name = 'asicrs' WHERE driver_name = 'pyasic';
