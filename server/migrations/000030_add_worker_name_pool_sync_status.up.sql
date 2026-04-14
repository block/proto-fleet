CREATE TYPE worker_name_pool_sync_status_enum AS ENUM (
    'POOL_UPDATED_SUCCESSFULLY'
);

ALTER TABLE device
ADD COLUMN worker_name_pool_sync_status worker_name_pool_sync_status_enum NULL;
