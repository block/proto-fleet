export type InfraDeviceStatus = "online" | "offline";
export type InfraDeviceEnabledMode = "off" | "auto";
export type InfraDeviceConnectionType = "modbus_tcp" | "mqtt_bridge" | "http_api";

export interface InfraDeviceItem {
  id: string;
  name: string;
  buildingName: string;
  siteName: string;
  connectionType: InfraDeviceConnectionType;
  endpoint: string;
  port: number;
  status: InfraDeviceStatus;
  enabled: InfraDeviceEnabledMode;
  lastSeen: string;
  fanCount?: number;
  endpointKind?: "single_fan" | "fan_group";
}

export type InfraDeviceDraft = Pick<
  InfraDeviceItem,
  "name" | "buildingName" | "siteName" | "connectionType" | "endpoint" | "port"
>;
