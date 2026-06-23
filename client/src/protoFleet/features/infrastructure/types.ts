export interface InfraDeviceItem {
  id: string;
  name: string;
  deviceType: string;
  subtype: string;
  model: string;
  buildingName: string;
  siteName: string;
  ipAddress: string;
  status: string;
  issues: number;
  rpm: number | null;
  powerW: number | null;
  temperatureC: number | null;
  firmware: string;
  lastSeen: string;
}

export interface DiscoveredInfraDevice {
  ipAddress: string;
  name: string;
  deviceType: string;
  subtype: string;
}
