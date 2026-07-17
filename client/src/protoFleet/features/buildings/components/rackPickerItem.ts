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
  // Ineligible for a plain add — the rack is in another building or another
  // site. Rendered disabled while the "Show assigned racks" toggle is off; when
  // the toggle is on the picker keeps these rows selectable (behind a reparent
  // confirm) so `disabled` alone no longer decides interactivity.
  disabled: boolean;
  // True for the same ineligible set (`inOtherBuilding || inOtherSite`).
  // Distinct from `disabled` because the toggle-on flow flips `disabled` off for
  // these rows but still needs to flag them and gate them behind the confirm.
  reassignment: boolean;
  // Miners currently placed in this rack; they move with it on reparent, so the
  // confirm copy states the count ("…and its N miners…").
  minerCount: number;
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
  return {
    id: rack.id.toString(),
    label: rack.label,
    buildingLabel,
    statusLabel,
    disabled,
    reassignment: disabled,
    minerCount: rack.deviceCount,
  };
};

/** Per-row conflict-dialog copy for a reassignment (already-placed) rack,
 *  surfaced when the operator taps the warning icon while "Show assigned racks"
 *  is on. States where the rack lives now and that its miners move with it. */
export const describeRackReassignment = (item: RackPickerItem, buildingName: string): string => {
  const where = item.statusLabel === "In another site" ? "another site" : "another building";
  const miners = item.minerCount === 1 ? "its 1 miner" : `its ${item.minerCount} miners`;
  return `Rack "${item.label || "(unnamed rack)"}" is currently in ${where}. Assigning it to "${buildingName}" will move the rack and ${miners} out of ${where === "another site" ? "that site" : "that building"}.`;
};
