import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { AssigneeOption, SiteOption } from "../types";
import { MaintenanceSiteOptionsScope } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useMaintenanceApi } from "@/protoFleet/api/maintenance";
import { useSites } from "@/protoFleet/api/sites";
import { useHasPermission, useUsername } from "@/protoFleet/store";

export const MaintenanceOptionsContext = createContext<ReturnType<typeof useLoadMaintenanceOptions> | null>(null);

export const useMaintenanceOptions = () => {
  const options = useContext(MaintenanceOptionsContext);
  if (!options) throw new Error("MaintenanceOptionsProvider is required");
  return options;
};

export const useLoadMaintenanceOptions = () => {
  const username = useUsername();
  const canManage = useHasPermission("maintenance:manage");
  const { listAssignees } = useMaintenanceApi();
  const { listSites } = useSites();
  const [assignees, setAssignees] = useState<AssigneeOption[]>([]);
  const [sites, setSites] = useState<SiteOption[]>([]);
  const [manageableSites, setManageableSites] = useState<SiteOption[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const requestSequence = useRef(0);
  const loaded = useRef(false);

  const load = useCallback(async () => {
    const sequence = ++requestSequence.current;
    if (!loaded.current) setLoading(true);

    let nextAssignees: AssigneeOption[] | undefined;
    let nextSites: SiteOption[] | undefined;
    let nextManageableSites: SiteOption[] | undefined = canManage ? undefined : [];
    let nextError: string | null = null;
    const loadManageableSites = canManage
      ? listSites({
          maintenanceOptionsScope: MaintenanceSiteOptionsScope.MANAGE,
          onSuccess: (items) => {
            nextManageableSites = items.flatMap((item) =>
              item.site ? [{ id: item.site.id.toString(), name: item.site.name }] : [],
            );
          },
          onError: (message) => {
            nextError = nextError ?? message;
          },
        })
      : Promise.resolve();
    await Promise.all([
      listAssignees({
        onSuccess: (items) => {
          nextAssignees = items.map((item) => ({
            id: item.userId.toString(),
            username: item.username,
            roleName: item.roleName,
          }));
        },
        onError: (message) => {
          nextError = message;
        },
      }),
      listSites({
        maintenanceOptionsScope: MaintenanceSiteOptionsScope.READ,
        onSuccess: (items) => {
          nextSites = items.flatMap((item) =>
            item.site ? [{ id: item.site.id.toString(), name: item.site.name }] : [],
          );
        },
        onError: (message) => {
          nextError = nextError ?? message;
        },
      }),
      loadManageableSites,
    ]);

    if (sequence !== requestSequence.current) return;
    if (nextAssignees) setAssignees(nextAssignees);
    if (nextSites) setSites(nextSites);
    if (nextManageableSites) setManageableSites(nextManageableSites);
    setError(nextError);
    loaded.current = true;
    setLoading(false);
  }, [canManage, listAssignees, listSites]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void load();
    });
    return () => {
      active = false;
      requestSequence.current += 1;
    };
  }, [load]);

  const currentAssignee = useMemo(
    () => assignees.find((item) => item.username === username) ?? null,
    [assignees, username],
  );
  return {
    assignees,
    sites,
    manageableSites,
    currentAssignee,
    loading,
    error,
    refresh: load,
  };
};
