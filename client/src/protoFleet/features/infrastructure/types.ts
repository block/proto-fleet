export type InfraDeviceKind = "single_fan" | "fan_group";

// UI projection of infrastructure.v1.InfrastructureDevice. driverConfig
// is the opaque JSON blob owned by the driver adapter; it is empty for
// site:read-only callers (the server redacts OT connection details), so
// consumers must degrade gracefully when it cannot be parsed.
export interface InfraDeviceItem {
  id: string;
  siteId: string;
  siteName: string;
  buildingName: string;
  name: string;
  deviceKind: InfraDeviceKind;
  fanCount: number;
  enabled: boolean;
  driverType: string;
  driverConfig: string;
}

export interface InfraBuildingOption {
  siteName: string;
  buildingName: string;
}

// Create payload produced by the add modal. The site is carried by name
// (the form works with catalog names); the page translates it to a site
// ID before calling the API.
export interface InfraDeviceDraft {
  name: string;
  siteName: string;
  buildingName: string;
  deviceKind: InfraDeviceKind;
  fanCount: number;
  driverType: string;
  driverConfig: string;
}

// Update payload produced by the detail modal (full-row update; the
// server treats every field except enabled as required). enabled is
// omitted unless the operator actually touched the switch in this
// modal session, so the server preserves the stored value instead of
// resending a possibly-stale snapshot.
export interface InfraDeviceUpdate extends InfraDeviceDraft {
  id: string;
  enabled?: boolean;
}
