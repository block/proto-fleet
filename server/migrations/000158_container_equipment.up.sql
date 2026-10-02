-- Latest observations only; equipment belongs to its paired controller.
CREATE TABLE container_fan_observation (
 device_id bigint NOT NULL REFERENCES device(id) ON DELETE CASCADE,
 component_id text NOT NULL,
 observed_at timestamptz NOT NULL,
 speed_percent double precision CHECK (speed_percent BETWEEN 0 AND 100),
 power_w double precision CHECK (power_w >= 0 AND power_w < 'Infinity'::double precision),
 direction text,
 alarm_active boolean,
 PRIMARY KEY (device_id, component_id)
);
CREATE TABLE container_controls_observation (
 device_id bigint PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
 observed_at timestamptz NOT NULL,
 supported boolean,
 main_circuit_breaker_supported boolean,
 main_circuit_breaker_closed boolean,
 alarm_reset_supported boolean,
 alarm_mute_supported boolean,
 dry_cooler_fan_reverse_supported boolean
);
CREATE TABLE container_control_observation (
 device_id bigint NOT NULL REFERENCES container_controls_observation(device_id) ON DELETE CASCADE,
 name text NOT NULL,
 enabled boolean,
 PRIMARY KEY (device_id, name)
);
CREATE TABLE container_setpoint_observation (
 device_id bigint NOT NULL REFERENCES container_controls_observation(device_id) ON DELETE CASCADE,
 name text NOT NULL,
 value double precision,
 unit text NOT NULL,
 PRIMARY KEY (device_id, name)
);
