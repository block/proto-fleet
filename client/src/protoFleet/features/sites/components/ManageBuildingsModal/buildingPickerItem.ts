// Row-shape + eligibility builder for ManageBuildingsModal. Classifies a
// BuildingWithCounts against the site eligibility rules (in-this-site /
// in-another-site / unassigned) and renders Name + Site + Status columns.
// Mirrors rackPickerItem so the building picker stays visually consistent
// with the rack picker.

import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";

export interface BuildingPickerItem {
  id: string;
  label: string;
  siteLabel: string;
  statusLabel: string;
  disabled: boolean;
}

// Other-site rows stay visible but disabled: cross-site moves require an
// explicit change-site action, rather than a picker selection.
const sitePlacement = (
  siteId: bigint | undefined,
  currentSiteId: bigint,
  siteLabels: Record<string, string>,
  fallbackLabel = "—",
) => {
  if (siteId === undefined || siteId === 0n) {
    return { siteLabel: "—", statusLabel: "Unassigned", disabled: false };
  }
  const inThisSite = siteId === currentSiteId;
  return {
    siteLabel: siteLabels[siteId.toString()] ?? fallbackLabel,
    statusLabel: inThisSite ? "In this site" : "In another site",
    disabled: !inThisSite,
  };
};

export const buildBuildingPickerItem = (
  row: BuildingWithCounts,
  currentSiteId: bigint,
  siteLabels: Record<string, string>,
): BuildingPickerItem | null => {
  const building = row.building;
  if (!building) return null;
  return {
    id: building.id.toString(),
    label: building.name,
    ...sitePlacement(building.siteId, currentSiteId, siteLabels),
  };
};

export const containerPickerId = (identifier: string) => `container:${identifier}`;

export const buildContainerPickerItem = (
  container: Container,
  currentSiteId: bigint,
  siteLabels: Record<string, string>,
): BuildingPickerItem => ({
  id: containerPickerId(container.deviceIdentifier),
  label: container.name || container.deviceIdentifier,
  ...sitePlacement(container.site?.id, currentSiteId, siteLabels, container.site?.label),
});
