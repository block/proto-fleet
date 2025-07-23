ALTER TABLE `device_ip_assignment`
ADD COLUMN `url_scheme` varchar(10) NOT NULL DEFAULT 'http' AFTER `port`;