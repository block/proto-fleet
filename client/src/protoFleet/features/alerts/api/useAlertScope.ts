import { useCallback, useState } from "react";
import type { RuleScope } from "@/protoFleet/features/alerts/types";

export interface UseAlertScopeResult {
  siteIds: string[];
  buildingIds: string[];
  rackIds: string[];
  groupIds: string[];
  deviceIds: string[];
  allSites: boolean;
  setSiteIds: (ids: string[]) => void;
  setBuildingIds: (ids: string[]) => void;
  setRackIds: (ids: string[]) => void;
  setGroupIds: (ids: string[]) => void;
  setDeviceIds: (ids: string[]) => void;
  setAllSites: (all: boolean) => void;
  // True when nothing is selected: the rule covers the whole org.
  isOrgWide: boolean;
  // Back to org-wide (the miner picker's "select all" lands here).
  clearAll: () => void;
  // Seed from a rule's scope (or null for create defaults); hosts call it from their open-sync block.
  reset: (scope: RuleScope | null | undefined) => void;
  // Undefined when org-wide so the config omits the field on the wire, like the server does.
  toRuleScope: () => RuleScope | undefined;
}

// Owns the "Apply to" scope state for the Add/Edit rule dialog.
export function useAlertScope(): UseAlertScopeResult {
  const [siteIds, setSiteIds] = useState<string[]>([]);
  const [buildingIds, setBuildingIds] = useState<string[]>([]);
  const [rackIds, setRackIds] = useState<string[]>([]);
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [deviceIds, setDeviceIds] = useState<string[]>([]);
  const [allSites, setAllSites] = useState(false);

  const isOrgWide =
    !allSites &&
    siteIds.length === 0 &&
    buildingIds.length === 0 &&
    rackIds.length === 0 &&
    groupIds.length === 0 &&
    deviceIds.length === 0;

  const reset = useCallback((scope: RuleScope | null | undefined) => {
    setSiteIds(scope?.site_ids ?? []);
    setBuildingIds(scope?.building_ids ?? []);
    setRackIds(scope?.rack_ids ?? []);
    setGroupIds(scope?.group_ids ?? []);
    setDeviceIds(scope?.device_ids ?? []);
    setAllSites(scope?.all_sites ?? false);
  }, []);

  const clearAll = useCallback(() => reset(null), [reset]);

  const toRuleScope = useCallback((): RuleScope | undefined => {
    if (isOrgWide) return undefined;
    return {
      site_ids: allSites ? [] : [...siteIds],
      device_ids: [...deviceIds],
      building_ids: [...buildingIds],
      rack_ids: [...rackIds],
      group_ids: [...groupIds],
      all_sites: allSites,
    };
  }, [isOrgWide, allSites, siteIds, buildingIds, rackIds, groupIds, deviceIds]);

  return {
    siteIds,
    buildingIds,
    rackIds,
    groupIds,
    deviceIds,
    allSites,
    setSiteIds,
    setBuildingIds,
    setRackIds,
    setGroupIds,
    setDeviceIds,
    setAllSites,
    isOrgWide,
    clearAll,
    reset,
    toRuleScope,
  };
}
