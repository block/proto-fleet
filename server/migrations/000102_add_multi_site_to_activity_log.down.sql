ALTER TABLE activity_log
    DROP CONSTRAINT ck_activity_log_multi_site_requires_null_site,
    DROP COLUMN multi_site;
