import { useCallback, useEffect, useMemo, useState } from "react";
import type { AssigneeOption, SiteOption } from "../types";
import { useMaintenanceApi } from "@/protoFleet/api/maintenance";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { useUsername } from "@/protoFleet/store";

export const useMaintenanceOptions = () => {
  const { sites, sitesError } = useSitesContext();
  const username = useUsername();
  const { listAssignees } = useMaintenanceApi();
  const [assignees, setAssignees] = useState<AssigneeOption[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    await listAssignees({
      onSuccess: (items) =>
        setAssignees(
          items.map((item) => ({ id: item.userId.toString(), username: item.username, roleName: item.roleName })),
        ),
      onError: setError,
      onFinally: () => setLoading(false),
    });
  }, [listAssignees]);
  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void load();
    });
    return () => {
      active = false;
    };
  }, [load]);
  const siteOptions = useMemo<SiteOption[]>(
    () => (sites ?? []).flatMap((item) => (item.site ? [{ id: item.site.id.toString(), name: item.site.name }] : [])),
    [sites],
  );
  const currentAssignee = useMemo(
    () => assignees.find((item) => item.username === username) ?? null,
    [assignees, username],
  );
  return {
    assignees,
    sites: siteOptions,
    currentAssignee,
    loading: loading || sites === undefined,
    error: error ?? sitesError,
    refresh: load,
  };
};
