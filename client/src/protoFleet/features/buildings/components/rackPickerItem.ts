// Shared row-shape + eligibility builder for the two rack pickers
// (ManageRacksModal bulk select, SearchRacksModal single select).
// Both classify a DeviceSet against the same eligibility rules
// (in-this-building / in-another-building / in-another-site /
// unassigned) and render the same Name + Building + Status columns,
// so the row builder lives here to avoid drift between the two
// surfaces.

import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

export interface RackPickerItem {
  id: string;
  label: string;
  buildingLabel: string;
  statusLabel: string;
  disabled: boolean;
}

export const buildRackPickerItem = (
  rack: DeviceSet,
  currentSiteId: bigint,
  currentBuildingId: bigint,
  buildingLabels: Record<string, string>,
): RackPickerItem | null => {
  if (rack.typeDetails.case !== "rackInfo") return null;
  const info = rack.typeDetails.value;
  const buildingId = info.buildingId;
  const siteId = info.siteId;
  const inOtherBuilding = buildingId !== undefined && buildingId !== 0n && buildingId !== currentBuildingId;
  const inThisBuilding = buildingId === currentBuildingId;
  // Racks under a *different* site are ineligible because moving them
  // across sites is a separate operator decision; the rack pickers
  // should only add racks that already share this building's site or
  // are unassigned entirely.
  const inOtherSite = !inThisBuilding && siteId !== undefined && siteId !== 0n && siteId !== currentSiteId;
  // Ineligible-but-visible: racks in another building or another site
  // render disabled so the operator sees why they can't be added.
  const disabled = inOtherBuilding || inOtherSite;
  const statusLabel = inOtherBuilding
    ? "In another building"
    : inOtherSite
      ? "In another site"
      : inThisBuilding
        ? "In this building"
        : "Unassigned";
  const buildingLabel =
    buildingId === undefined || buildingId === 0n ? "—" : (buildingLabels[buildingId.toString()] ?? "—");
  return { id: rack.id.toString(), label: rack.label, buildingLabel, statusLabel, disabled };
};
