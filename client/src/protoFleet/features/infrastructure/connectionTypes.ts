import type { InfraDeviceConnectionType } from "./types";

export const MODBUS_TCP_CONNECTION_TYPE: InfraDeviceConnectionType = "modbus_tcp";
export const MODBUS_TCP_CONNECTION_TYPE_LABEL = "Modbus TCP";

export const infraDeviceConnectionTypeOptions: { value: InfraDeviceConnectionType; label: string }[] = [
  { value: MODBUS_TCP_CONNECTION_TYPE, label: MODBUS_TCP_CONNECTION_TYPE_LABEL },
];

export const getInfraDeviceConnectionTypeLabel = (connectionType: InfraDeviceConnectionType) =>
  infraDeviceConnectionTypeOptions.find((option) => option.value === connectionType)?.label ?? connectionType;
