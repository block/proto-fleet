import { useCallback, useRef, useState } from "react";

import {
  type BuildingFormValues,
  buildingFormValuesFromBuilding,
  emptyBuildingFormValues,
  useBuildings,
} from "@/protoFleet/api/buildings";
import { type Building, type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Building modal stack. BuildingSettingsModal can render alone (entry from
// the ManageSiteModal buildings table) or stacked on
// top of ManageBuildingModal (entry from /buildings/:id "Edit building"
// header).
//
// Edit-bearing states carry a `row: BuildingWithCounts` so the cascade
// dialog has rack_count without a second round-trip. Create state only
// has a draft + parent site context — there's no row yet.
//
// deleteTarget lives in a parallel field so the cascade dialog reads as
// the topmost surface while leaving whatever modal sat underneath intact
// — matching the SiteModals pattern.
export type BuildingModalState =
  | { kind: "none" }
  // siteId undefined when opened from the global Buildings-tab CTA with no
  // scope — the modal renders an empty Site dropdown for the operator to pick.
  // When set (entry from /sites/:id, a site-scoped row, or a page-header site
  // scope), the dropdown locks to that site.
  | { kind: "detailsCreate"; siteId?: bigint; siteName?: string; draft: BuildingFormValues }
  | { kind: "detailsEdit"; row: BuildingWithCounts; siteName?: string; draft: BuildingFormValues }
  | { kind: "manage"; row: BuildingWithCounts; siteName?: string }
  | { kind: "manageEditingDetails"; row: BuildingWithCounts; siteName?: string; draft: BuildingFormValues };

interface UseBuildingModalsOptions {
  // Refetches the host page's buildings cache. Called after every successful
  // mutation so ManageSiteModal stays in sync without the
  // host wiring its own refetch into every callback.
  refetchBuildings?: () => void;
  // Optional host-level follow-up when a successful building mutation also
  // needs to refresh adjacent page state, like parent-site summary counts.
  onMutationSuccess?: () => void;
  // Fires when a delete originating from ManageBuildingModal succeeds. The
  // manage modal's anchor is the now-deleted building, so the host page
  // (typically /buildings/:id) needs to navigate elsewhere — this hook
  // can't know whether the caller is the building page, a site page, or
  // somewhere else, so the redirect decision stays with the host.
  onDeleteFromManage?: (deletedBuildingId: bigint) => void;
}

// Extracts the underlying Building from BuildingWithCounts. The proto field
// is technically optional but every row returned by ListBuildings has one
// set; the assertion keeps callers honest and surfaces malformed rows fast.
const unwrap = (row: BuildingWithCounts): Building => {
  if (!row.building) throw new Error("BuildingWithCounts missing building field");
  return row.building;
};

export interface BuildingModalsApi {
  state: BuildingModalState;
  deleteTarget: BuildingWithCounts | null;
  saving: boolean;
  deleting: boolean;
  // siteId optional so the Buildings-tab CTA can open the modal without
  // a parent-site context — the dropdown inside the modal collects it.
  openDetailsCreate: (siteId?: bigint, siteName?: string) => void;
  openDetailsEdit: (row: BuildingWithCounts, siteName?: string) => void;
  // unassignedMinerCount surfaces the count-line in ManageBuildingModal when
  // the building was created from a bulk "New building" action seeded with
  // loose miners. Omitted by every normal edit caller → no count line.
  openManage: (row: BuildingWithCounts, siteName?: string, unassignedMinerCount?: number) => void;
  // Count carried alongside the manage state for the seeded-create flow.
  manageUnassignedMinerCount: number | undefined;
  // Closes the topmost modal: drops details if details is stacked on manage,
  // otherwise collapses to none. Mirrors useSiteModals.dismiss.
  dismiss: () => void;
  dismissDeleteConfirm: () => void;
  // BuildingSettingsModal handlers. Create returns the created Building so
  // hosts that want to chain (e.g. open ManageBuildingModal on the new
  // building) can do so; today every caller just closes the modal.
  // siteId is collected by the modal itself (either pre-filled from the
  // caller or chosen from the Site dropdown), so callers pass it through
  // to the mutation rather than relying on state.siteId.
  detailsCreate: (values: BuildingFormValues, siteId: bigint) => Promise<Building | null>;
  detailsSaveEdit: (values: BuildingFormValues) => Promise<Building | null>;
  // Stack details (edit) on top of manage. Used by the ManageBuildingModal
  // header's "Edit building" button.
  manageEditDetails: () => void;
  // Trigger the cascade dialog for the currently-edited building. No cache
  // lookup needed — the row lives on `state`.
  requestDeleteCurrent: () => void;
  deleteConfirm: () => Promise<void>;
  // Refetch the host's building cache. Exposed so ManageBuildingModal
  // can ping it after a successful rack-placement save (rack_count and
  // grid positions change without touching create/update/delete paths).
  refreshBuildings: () => void;
}

const useBuildingModals = ({
  refetchBuildings,
  onMutationSuccess,
  onDeleteFromManage,
}: UseBuildingModalsOptions = {}): BuildingModalsApi => {
  const [state, setState] = useState<BuildingModalState>({ kind: "none" });
  const [deleteTarget, setDeleteTarget] = useState<BuildingWithCounts | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // Set by openManage; read while the manage modal is open. Stale values
  // while closed are harmless (the modal isn't rendered), and the next
  // openManage overwrites — so no explicit reset is needed.
  const [manageUnassignedMinerCount, setManageUnassignedMinerCount] = useState<number | undefined>(undefined);

  // Synchronous in-flight guard so the disabled-prop lag on the button
  // (setState batching) can't slip a double-click past us.
  const savingRef = useRef(false);
  // Tracks whether the pending delete originated from inside
  // ManageBuildingModal. Captured at requestDeleteCurrent time because the
  // state transitions away (drops details) before the operator confirms.
  const deleteFromManageRef = useRef(false);

  const { createBuilding, updateBuilding, deleteBuilding } = useBuildings();

  const openDetailsCreate = useCallback((siteId?: bigint, siteName?: string) => {
    setState({ kind: "detailsCreate", siteId, siteName, draft: emptyBuildingFormValues() });
  }, []);

  const openDetailsEdit = useCallback((row: BuildingWithCounts, siteName?: string) => {
    setState({ kind: "detailsEdit", row, siteName, draft: buildingFormValuesFromBuilding(unwrap(row)) });
  }, []);

  const openManage = useCallback((row: BuildingWithCounts, siteName?: string, unassignedMinerCount?: number) => {
    setManageUnassignedMinerCount(unassignedMinerCount);
    setState({ kind: "manage", row, siteName });
  }, []);

  const dismiss = useCallback(() => {
    setState((prev) => {
      if (prev.kind === "manageEditingDetails") {
        return { kind: "manage", row: prev.row, siteName: prev.siteName };
      }
      return { kind: "none" };
    });
  }, []);

  const dismissDeleteConfirm = useCallback(() => {
    setDeleteTarget(null);
    deleteFromManageRef.current = false;
  }, []);

  const manageEditDetails = useCallback(() => {
    setState((prev) => {
      if (prev.kind === "manage") {
        return {
          kind: "manageEditingDetails",
          row: prev.row,
          siteName: prev.siteName,
          draft: buildingFormValuesFromBuilding(unwrap(prev.row)),
        };
      }
      return prev;
    });
  }, []);

  const detailsCreate = useCallback(
    async (values: BuildingFormValues, siteId: bigint): Promise<Building | null> => {
      if (savingRef.current) return null;
      if (state.kind !== "detailsCreate") return null;
      savingRef.current = true;
      setSaving(true);
      return await new Promise<Building | null>((resolve) => {
        void createBuilding({
          values,
          siteId,
          onSuccess: (building) => {
            pushToast({ message: `Building "${building.name}" created`, status: STATUSES.success });
            refetchBuildings?.();
            onMutationSuccess?.();
            // Functional setState so a mid-flight dismiss can't be overwritten
            // by this success closure.
            setState((prev) => (prev.kind === "detailsCreate" ? { kind: "none" } : prev));
            resolve(building);
          },
          onError: (msg) => {
            pushToast({ message: `Failed to create building: ${msg}`, status: STATUSES.error });
            resolve(null);
          },
          onFinally: () => {
            savingRef.current = false;
            setSaving(false);
          },
        });
      });
    },
    [state, createBuilding, refetchBuildings, onMutationSuccess],
  );

  const detailsSaveEdit = useCallback(
    async (values: BuildingFormValues): Promise<Building | null> => {
      if (savingRef.current) return null;
      // detailsEdit (standalone) and manageEditingDetails (stacked) both
      // resolve to UpdateBuilding — they just unwind to different surfaces.
      let id: bigint | null = null;
      if (state.kind === "detailsEdit") id = unwrap(state.row).id;
      else if (state.kind === "manageEditingDetails") id = unwrap(state.row).id;
      if (id === null) return null;

      savingRef.current = true;
      setSaving(true);
      return await new Promise<Building | null>((resolve) => {
        void updateBuilding({
          id,
          values,
          onSuccess: (building) => {
            pushToast({ message: `Building "${building.name}" saved`, status: STATUSES.success });
            refetchBuildings?.();
            onMutationSuccess?.();
            setState((prev) => {
              if (prev.kind === "detailsEdit") return { kind: "none" };
              if (prev.kind === "manageEditingDetails") {
                // Refresh the row so the underlying manage modal sees the
                // canonical server response (name / power, etc.).
                const refreshedRow: BuildingWithCounts = { ...prev.row, building };
                return { kind: "manage", row: refreshedRow, siteName: prev.siteName };
              }
              return prev;
            });
            resolve(building);
          },
          onError: (msg) => {
            pushToast({ message: `Failed to save building: ${msg}`, status: STATUSES.error });
            resolve(null);
          },
          onFinally: () => {
            savingRef.current = false;
            setSaving(false);
          },
        });
      });
    },
    [state, updateBuilding, refetchBuildings, onMutationSuccess],
  );

  const requestDeleteCurrent = useCallback(() => {
    setState((prev) => {
      let row: BuildingWithCounts | null = null;
      let fromManage = false;
      if (prev.kind === "detailsEdit") row = prev.row;
      else if (prev.kind === "manage") {
        row = prev.row;
        fromManage = true;
      } else if (prev.kind === "manageEditingDetails") {
        row = prev.row;
        fromManage = true;
      }
      if (!row) return prev;
      setDeleteTarget(row);
      deleteFromManageRef.current = fromManage;
      // Drop the stacked details so the cascade dialog reads as the topmost
      // surface above whatever sat underneath. Cancel restores the underlying
      // state (manage or — for detailsEdit — none, since we don't keep
      // detailsEdit open behind the dialog).
      if (prev.kind === "manageEditingDetails") {
        return { kind: "manage", row: prev.row, siteName: prev.siteName };
      }
      if (prev.kind === "detailsEdit") {
        return { kind: "none" };
      }
      return prev;
    });
  }, []);

  const deleteConfirm = useCallback(async () => {
    if (!deleteTarget) return;
    const id = deleteTarget.building?.id;
    const name = deleteTarget.building?.name ?? "building";
    if (!id || id === 0n) return;
    const wasFromManage = deleteFromManageRef.current;

    setDeleting(true);
    await new Promise<void>((resolve) => {
      void deleteBuilding({
        id,
        onSuccess: () => {
          pushToast({ message: `Building "${name}" deleted`, status: STATUSES.success });
          refetchBuildings?.();
          onMutationSuccess?.();
          setDeleteTarget(null);
          deleteFromManageRef.current = false;
          setState({ kind: "none" });
          // Defer the redirect callback to a microtask so React commits
          // the state reset above before the host page navigates away.
          // Synchronous invocation here would race a route change against
          // the modal-close render and warn about state updates on an
          // unmounted host. Microtask is enough — we just need to clear
          // the current React work loop, not skip a paint.
          if (wasFromManage) {
            queueMicrotask(() => onDeleteFromManage?.(id));
          }
          resolve();
        },
        onError: (msg) => {
          pushToast({ message: `Failed to delete building: ${msg}`, status: STATUSES.error });
          resolve();
        },
        onFinally: () => setDeleting(false),
      });
    });
  }, [deleteTarget, deleteBuilding, refetchBuildings, onMutationSuccess, onDeleteFromManage]);

  const refreshBuildings = useCallback(() => {
    refetchBuildings?.();
  }, [refetchBuildings]);

  return {
    state,
    deleteTarget,
    saving,
    deleting,
    manageUnassignedMinerCount,
    openDetailsCreate,
    openDetailsEdit,
    openManage,
    dismiss,
    dismissDeleteConfirm,
    detailsCreate,
    detailsSaveEdit,
    manageEditDetails,
    requestDeleteCurrent,
    deleteConfirm,
    refreshBuildings,
  };
};

export { useBuildingModals };
