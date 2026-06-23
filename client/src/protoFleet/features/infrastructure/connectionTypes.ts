import type { InfraDeviceConnectionType } from "./types";

export const infraDeviceConnectionTypeOptions: { value: InfraDeviceConnectionType; label: string }[] = [
  { value: "modbus_tcp", label: "Modbus TCP" },
  { value: "mqtt_bridge", label: "MQTT bridge" },
  { value: "http_api", label: "HTTP/API" },
];

export const getInfraDeviceConnectionTypeLabel = (connectionType: InfraDeviceConnectionType) =>
  infraDeviceConnectionTypeOptions.find((option) => option.value === connectionType)?.label ?? connectionType;
