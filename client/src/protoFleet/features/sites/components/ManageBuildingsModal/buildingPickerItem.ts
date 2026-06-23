// Row-shape + eligibility builder for ManageBuildingsModal. Classifies a
// BuildingWithCounts against the site eligibility rules (in-this-site /
// in-another-site / unassigned) and renders Name + Site + Status columns.
// Mirrors rackPickerItem so the building picker stays visually consistent
// with the rack picker.

import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";

export interface BuildingPickerItem {
  id: string;
  label: string;
  siteLabel: string;
  statusLabel: string;
  disabled: boolean;
}

export const buildBuildingPickerItem = (
  row: BuildingWithCounts,
  currentSiteId: bigint,
  siteLabels: Record<string, string>,
): BuildingPickerItem | null => {
  const building = row.building;
  if (!building) return null;
  const siteId = building.siteId;
  const inThisSite = siteId !== undefined && siteId !== 0n && siteId === currentSiteId;
  // Buildings under a *different* site are ineligible because moving them
  // across sites cascades site_id down to every rack + device — a heavier
  // operator decision than this picker should make implicitly. The picker
  // only adds buildings that already share this site or are unassigned.
  const inOtherSite = siteId !== undefined && siteId !== 0n && siteId !== currentSiteId;
  // Ineligible-but-visible: buildings in another site render disabled so
  // the operator sees why they can't be added.
  const disabled = inOtherSite;
  const statusLabel = inOtherSite ? "In another site" : inThisSite ? "In this site" : "Unassigned";
  const siteLabel = siteId === undefined || siteId === 0n ? "—" : (siteLabels[siteId.toString()] ?? "—");
  return { id: building.id.toString(), label: building.name, siteLabel, statusLabel, disabled };
};
