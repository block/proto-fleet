export type InfraDeviceStatus = "running" | "stopped" | "faulted" | "unknown";
export type InfraDeviceEnabledMode = "off" | "auto";
export type InfraDeviceIssueStatus = "pending" | "acked" | "failed" | "timed_out";

export interface InfraDeviceItem {
  id: string;
  name: string;
  buildingName: string;
  siteName: string;
  endpoint: string;
  port: number;
  status: InfraDeviceStatus;
  enabled: InfraDeviceEnabledMode;
  issueStatus: InfraDeviceIssueStatus | null;
  lastSeen: string;
  fanCount?: number;
  endpointKind?: "single_fan" | "fan_group";
}
