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
  // The rack lives in a *different site*. A cross-site rack usually also has a
  // buildingId, so this is tracked explicitly (rather than inferred from the
  // status label) to describe the higher-stakes site move accurately.
  crossSite: boolean;
  // Miners currently placed in this rack; they move with it on reparent, so the
  // confirm copy states the count ("…and its N miners…").
  minerCount: number;
}

export const buildRackPickerItem = (
  rack: DeviceSet,
  currentSiteId: bigint,
  currentBuildingId: bigint,
  buildingLabels: Record<string, string>,
  // Ids already in this building's working set (the picker's seeded selection).
  // A seeded rack belongs to THIS building in the operator's draft — including a
  // reparent staged this session but not yet Saved, whose server row still
  // reports its old placement. Trust the draft over the stale server state:
  // classify it in-this-building so it renders selected + eligible, never as a
  // reassignment row that the drop-on-reassignment paths (toggle-off, Select
  // all, header deselect) could silently strip. Mirrors the miner picker, which
  // seeds from its in-memory draft rather than a re-fetch.
  seededRackIds?: ReadonlySet<string>,
): RackPickerItem | null => {
  if (rack.typeDetails.case !== "rackInfo") return null;
  const info = rack.typeDetails.value;
  const buildingId = info.buildingId;
  const siteId = info.siteId;
  const seeded = seededRackIds?.has(rack.id.toString()) ?? false;
  const inOtherBuilding = !seeded && buildingId !== undefined && buildingId !== 0n && buildingId !== currentBuildingId;
  const inThisBuilding = seeded || buildingId === currentBuildingId;
  // Racks under a *different* site are ineligible because moving them
  // across sites is a separate operator decision; the rack pickers
  // should only add racks that already share this building's site or
  // are unassigned entirely.
  const inOtherSite = !inThisBuilding && siteId !== undefined && siteId !== 0n && siteId !== currentSiteId;
  // Ineligible-but-visible: racks in another building or another site
  // render disabled so the operator sees why they can't be added.
  const disabled = inOtherBuilding || inOtherSite;
  // A cross-site rack usually also has a buildingId, so surface the site move
  // first — it is the higher-stakes reparent and the operator needs to know a
  // site boundary is being crossed, not just a building.
  const statusLabel = inOtherSite
    ? "In another site"
    : inOtherBuilding
      ? "In another building"
      : inThisBuilding
        ? "In this building"
        : "Unassigned";
  // A seeded reparent still carries its old buildingId; show THIS building's
  // label so the Building column agrees with the in-this-building status.
  const buildingLabel = seeded
    ? (buildingLabels[currentBuildingId.toString()] ?? "—")
    : buildingId === undefined || buildingId === 0n
      ? "—"
      : (buildingLabels[buildingId.toString()] ?? "—");
  return {
    id: rack.id.toString(),
    label: rack.label,
    buildingLabel,
    statusLabel,
    disabled,
    reassignment: disabled,
    crossSite: inOtherSite,
    minerCount: rack.deviceCount,
  };
};

/** Per-row conflict-dialog copy for a reassignment (already-placed) rack,
 *  surfaced when the operator taps the warning icon while "Show assigned racks"
 *  is on. States where the rack lives now and that its miners move with it. */
export const describeRackReassignment = (item: RackPickerItem, buildingName: string): string => {
  const where = item.crossSite ? "another site" : "another building";
  const miners = item.minerCount === 1 ? "its 1 miner" : `its ${item.minerCount} miners`;
  return `Rack "${item.label || "(unnamed rack)"}" is currently in ${where}. Assigning it to "${buildingName}" will move the rack and ${miners} out of ${item.crossSite ? "that site" : "that building"}.`;
};
