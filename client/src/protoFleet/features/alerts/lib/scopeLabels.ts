import { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";

export interface ScopeCounts {
  allSites: boolean;
  sites: number;
  buildings: number;
  racks: number;
  groups: number;
  miners: number;
  // Server withheld the device ids (no miner:read); the subset must still read as a subset.
  minersRedacted?: boolean;
}

// One label per populated scope dimension ("All sites", "2 sites"); empty for org-wide.
// Single owner of the dimension list so the editor summary and rules-table column can't drift.
export const scopePartLabels = ({
  allSites,
  sites,
  buildings,
  racks,
  groups,
  miners,
  minersRedacted,
}: ScopeCounts): string[] => {
  const parts: string[] = [];
  if (allSites) {
    parts.push("All sites");
  } else if (sites > 0) {
    parts.push(getTargetButtonLabel(sites, "site"));
  }
  if (buildings > 0) parts.push(getTargetButtonLabel(buildings, "building"));
  if (racks > 0) parts.push(getTargetButtonLabel(racks, "rack"));
  if (groups > 0) parts.push(getTargetButtonLabel(groups, "group"));
  if (miners > 0) {
    parts.push(getTargetButtonLabel(miners, "miner"));
  } else if (minersRedacted) {
    parts.push("selected miners (restricted)");
  }
  return parts;
};
