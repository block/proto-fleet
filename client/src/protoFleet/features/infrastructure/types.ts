export type InfraDeviceKind = "single_fan" | "fan_group";

// Device kind as carried on the wire. Known kinds keep literal-type
// support, but a kind from a newer server is preserved verbatim rather
// than silently normalized to a known kind — a save that echoes an
// unknown kind fails loudly against the server's device_kind
// validation instead of corrupting the row.
export type InfraDeviceKindWire = InfraDeviceKind | (string & {});

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
  deviceKind: InfraDeviceKindWire;
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

// Update payload produced by the detail modal. Every field except id is
// optional and present only when the operator actually changed it in
// this modal session — the update path fetches the device's fresh row
// and fills the rest from there, so a stale modal can't silently
// overwrite another operator's concurrent edit (the wire update RPC is
// full-row). siteName stays a catalog name and is only resolved to an
// ID when the operator picked a different site; otherwise the fresh
// row's immutable siteId is reused, keeping unchanged saves independent
// of the site catalog.
export interface InfraDevicePatch {
  id: string;
  name?: string;
  siteName?: string;
  buildingName?: string;
  enabled?: boolean;
  driverConfig?: string;
}
