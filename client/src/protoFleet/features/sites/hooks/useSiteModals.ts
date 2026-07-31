import { useCallback, useEffect, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import {
  type BuildingFormValues,
  type BulkCreateBuildingError,
  type NewBuildingInput,
  useBuildings,
} from "@/protoFleet/api/buildings";
import { type Building } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues, type SiteFormValues, siteFormValuesFromSite, useSites } from "@/protoFleet/api/sites";
import { scopeCurrentOrDashboardPath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Modal-stack state. deleteConfirm lives in a parallel field (not this union)
// so the cascade dialog renders as a sibling that overlays the stacked
// manage/details modals without unmounting them — mirroring ManageRackModal's
// pattern. Cancel on the cascade dialog returns the operator to whichever
// modal they came from.
export type SiteModalState =
  | { kind: "none" }
  // Initial create step. "Continue" now persists the site (CreateSite) and
  // transitions straight to manageEdit against the real row, so the manage
  // surface always has a site id — inline building-create and
  // AssignBuildingsToSite both need one. Mirrors the seeded "New site" flow in
  // FleetCreateFlowProvider, which already creates-then-openManageEdit.
  | { kind: "detailsCreate"; draft: SiteFormValues }
  | { kind: "manageEdit"; site: Site; draft: SiteFormValues }
  // Stacked: ManageSiteModal stays open while SiteSettingsModal renders on top.
  // Save calls UpdateSite directly; on success details closes and manage stays
  // open with refreshed draft.
  | { kind: "manageEditEditingDetails"; site: Site; draft: SiteFormValues }
  // The buildings picker on its own, with no manage surface behind it — for
  // hosts that already show the site (the detail page) and only want to edit
  // membership. Carries the site because the write handlers below resolve their
  // target from state, not from an argument, and the host's current membership
  // because there is no ManageSiteModal working set to read it from — the ids
  // seed the picker's selection, the names feed the bulk-create collision check.
  | { kind: "buildingsPicker"; site: Site; currentBuildings: SiteBuildingRef[] };

// Minimum a host has to hand over about the buildings already in the site.
export interface SiteBuildingRef {
  id: bigint;
  name: string;
}

// The site a write should target, for every state that has one. Extracted so
// the guard lives once: each handler previously repeated the same pair of kind
// checks, which is why adding buildingsPicker would otherwise have meant
// editing four call sites and silently no-op'ing at any one that was missed.
const managedSite = (state: SiteModalState): Site | null => {
  switch (state.kind) {
    case "manageEdit":
    case "manageEditEditingDetails":
    case "buildingsPicker":
      return state.site;
    default:
      return null;
  }
};

interface UseSiteModalsOptions {
  refetchSites: () => void;
  // Bumps the page's building-refresh signal so any sibling building list
  // (e.g. SiteSettingsSingleView's table) re-fetches after a membership
  // change. Optional so hosts without a building table can omit it.
  refetchBuildings?: () => void;
}

// Outcome of a bulk building create. `created` is empty whenever the batch was
// rejected (or the dispatch was skipped), which is what tells the modal to stay
// open; `errors` carries the offending rows when the server named them.
export interface BulkBuildingCreateResult {
  created: Building[];
  errors: BulkCreateBuildingError[];
}

export interface SiteModalsApi {
  state: SiteModalState;
  // SiteWithCounts row when the cascade dialog should be shown. Null when no
  // delete is pending. Lives outside `state` so dismissing the dialog
  // returns the operator to whichever manage/details modal they came from.
  deleteTarget: SiteWithCounts | null;
  saving: boolean;
  deleting: boolean;
  openCreate: () => void;
  // unassigned*Count surface count-lines in ManageSiteModal when the site was
  // created from a bulk "New site" action seeded with loose racks/miners.
  // Omitted by normal edit callers → no count lines.
  openManageEdit: (site: Site, opts?: { unassignedRackCount?: number; unassignedMinerCount?: number }) => void;
  // Opens the buildings picker with nothing behind it. For hosts that already
  // render the site (the detail page), where stacking the whole manage surface
  // under a membership edit is a layer the operator didn't ask for.
  openBuildingsPicker: (site: Site, currentBuildings: SiteBuildingRef[]) => void;
  manageUnassignedRackCount: number | undefined;
  manageUnassignedMinerCount: number | undefined;
  // Resolve a SiteWithCounts from the page's sites cache and open the
  // cascade dialog. The hook does the lookup so callers don't duplicate the
  // same id-matching logic.
  requestDeleteCurrent: (sites: SiteWithCounts[] | undefined) => void;
  // Closes the topmost modal: drops details if details is stacked on
  // manage, otherwise closes everything to none.
  dismiss: () => void;
  // SiteDeleteDialog onDismiss — closes only the cascade dialog.
  dismissDeleteConfirm: () => void;
  // SiteSettingsModal handlers. Continue persists the site (CreateSite) and,
  // on success, opens ManageSiteModal in edit mode against the new row.
  detailsContinueCreate: (values: SiteFormValues) => Promise<void>;
  detailsSaveEdit: (values: SiteFormValues) => Promise<void>;
  // ManageSiteModal handlers
  manageEditDetails: () => void;
  // Inline building-create from ManageSiteModal. Creates the building against
  // the currently-managed site via the transactional CreateBuilding RPC and
  // returns the created row so the modal can inject it into its working set.
  // Returns null on failure (a toast is shown); a duplicate name surfaces as
  // a server error and creates nothing.
  manageCreateBuilding: (values: BuildingFormValues) => Promise<Building | null>;
  // Bulk sibling of manageCreateBuilding. CreateBuildings is all-or-nothing, so
  // the result is either every row in `created` or none of them plus the
  // per-row reasons the modal marks its preview with.
  manageCreateBuildings: (buildings: NewBuildingInput[]) => Promise<BulkBuildingCreateResult>;
  // Create hand-offs for the standalone picker. Same RPCs as their manage*
  // siblings, but they refresh the host's building list: the manage* versions
  // skip that on purpose because ManageSiteModal injects the created rows into
  // its own working set, and here there is no working set to inject into.
  pickerCreateBuilding: (values: BuildingFormValues) => Promise<Building | null>;
  pickerCreateBuildings: (buildings: NewBuildingInput[]) => Promise<BulkBuildingCreateResult>;
  // Commit-per-modal: the buildings picker owns site membership, so its Save
  // applies the delta via AssignBuildingsToSite right away rather than staging
  // it into ManageSiteModal. `added` moves buildings into this site; `removed`
  // moves them to "Unassigned". Resolves true on success.
  manageAssignBuildings: (delta: { added: bigint[]; removed: bigint[] }) => Promise<boolean>;
  // Kebab "Remove building" — an immediate unassign (targetSiteId unset), so
  // the row action means what it says instead of queueing behind a Save.
  manageRemoveBuilding: (buildingId: bigint, label: string) => Promise<boolean>;
  // SiteDeleteDialog handlers
  deleteConfirm: () => Promise<void>;
}

const useSiteModals = ({ refetchSites, refetchBuildings }: UseSiteModalsOptions): SiteModalsApi => {
  const [state, setState] = useState<SiteModalState>({ kind: "none" });
  const [deleteTarget, setDeleteTarget] = useState<SiteWithCounts | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // Set by openManageEdit; read while the manage modal is open. Stale values
  // while closed are harmless, and the next openManageEdit overwrites.
  const [manageUnassignedRackCount, setManageUnassignedRackCount] = useState<number | undefined>(undefined);
  const [manageUnassignedMinerCount, setManageUnassignedMinerCount] = useState<number | undefined>(undefined);

  // Synchronous in-flight guard for Save dispatches. setState batching means
  // the `saving` prop driving the button's `disabled` lags one render behind
  // the click — a double-click would otherwise reach the dispatch path twice.
  const savingRef = useRef(false);
  // Mirror of the modal state for synchronous reads inside async
  // handlers. setState updaters can't be used as "reads" — React
  // treats them as pure functions and may defer or replay them, so a
  // ref synced after each commit is the right shape for guards that
  // need to check the *current* state at dispatch time.
  const stateRef = useRef(state);
  useEffect(() => {
    stateRef.current = state;
  }, [state]);

  const { createSite, updateSite, deleteSite, assignBuildingsToSite } = useSites();
  const { createBuilding, createBuildings } = useBuildings();
  const setActiveSite = useFleetStore((store) => store.ui.setActiveSite);
  const activeSite = useFleetStore((store) => store.ui.activeSite);
  // Signals the PageHeader's SitePicker (which fetches sites once on mount and
  // can't see this page's refetchSites) to refresh after a site is created,
  // renamed, or deleted.
  const bumpSitesRevision = useFleetStore((store) => store.ui.bumpSitesRevision);
  const routeScope = useRouteSiteScope();
  const navigate = useNavigate();
  const { pathname, search, hash } = useLocation();

  const openCreate = useCallback(() => {
    setState({ kind: "detailsCreate", draft: emptySiteFormValues() });
  }, []);

  const openManageEdit = useCallback(
    (site: Site, opts?: { unassignedRackCount?: number; unassignedMinerCount?: number }) => {
      setManageUnassignedRackCount(opts?.unassignedRackCount);
      setManageUnassignedMinerCount(opts?.unassignedMinerCount);
      setState({ kind: "manageEdit", site, draft: siteFormValuesFromSite(site) });
    },
    [],
  );

  const openBuildingsPicker = useCallback((site: Site, currentBuildings: SiteBuildingRef[]) => {
    setState({ kind: "buildingsPicker", site, currentBuildings });
  }, []);

  const requestDeleteCurrent = useCallback((sites: SiteWithCounts[] | undefined) => {
    // Pulls the currently-edited site id from state and resolves the matching
    // SiteWithCounts row from the page's list cache. Triggered when Delete is
    // clicked inside SiteSettingsModal (edit mode) or any future row-level
    // delete affordance.
    setState((prev) => {
      if (prev.kind !== "manageEdit" && prev.kind !== "manageEditEditingDetails") return prev;
      const id = prev.site.id.toString();
      const match = sites?.find((s) => (s.site?.id ?? 0n).toString() === id);
      if (!match) return prev;
      setDeleteTarget(match);
      // Drop the stacked details modal when the cascade dialog opens so the
      // dialog reads as the topmost surface above the persistent
      // ManageSiteModal. Cancelling the dialog returns to manageEdit.
      if (prev.kind === "manageEditEditingDetails") {
        return { kind: "manageEdit", site: prev.site, draft: prev.draft };
      }
      return prev;
    });
  }, []);

  const dismiss = useCallback(() => {
    // The stacked edit-details state drops just the top (details) and returns
    // to the underlying manage state. Everything else closes to none.
    setState((prev) => {
      if (prev.kind === "manageEditEditingDetails") {
        return { kind: "manageEdit", site: prev.site, draft: prev.draft };
      }
      return { kind: "none" };
    });
  }, []);

  const dismissDeleteConfirm = useCallback(() => {
    setDeleteTarget(null);
  }, []);

  // Continue on the create-details modal persists the site immediately, then
  // opens ManageSiteModal in edit mode against the new row. Creating the site
  // up front (rather than deferring to the manage modal's Save) gives inline
  // building-create a real site_id to attach to. On failure the details modal
  // stays open so the operator can fix the input and retry (e.g. a duplicate
  // name). Guarded so a double-click can't fire two CreateSite calls.
  const detailsContinueCreate = useCallback(
    async (values: SiteFormValues) => {
      if (savingRef.current) return;
      const current = stateRef.current;
      if (current.kind !== "detailsCreate") return;
      // Carry the existing networkConfig draft through; SiteSettingsModal only
      // owns the descriptive fields, so the value typed elsewhere survives.
      const draft: SiteFormValues = { ...values, networkConfig: current.draft.networkConfig };
      savingRef.current = true;
      setSaving(true);
      try {
        await new Promise<void>((resolve) => {
          void createSite({
            values: draft,
            onSuccess: ({ site, networkConfigWarnings }) => {
              pushToast({
                message:
                  networkConfigWarnings.length > 0
                    ? `Site "${site.name}" created with warnings`
                    : `Site "${site.name}" created`,
                status: STATUSES.success,
              });
              refetchSites();
              refetchBuildings?.();
              bumpSitesRevision();
              setState({ kind: "manageEdit", site, draft: siteFormValuesFromSite(site) });
              resolve();
            },
            onError: (msg) => {
              pushToast({ message: `Failed to create site: ${msg}`, status: STATUSES.error });
              resolve();
            },
          });
        });
      } finally {
        savingRef.current = false;
        setSaving(false);
      }
    },
    [createSite, refetchSites, refetchBuildings, bumpSitesRevision],
  );

  const detailsSaveEdit = useCallback(
    async (values: SiteFormValues) => {
      if (savingRef.current) return;
      // Read the current modal state synchronously via the ref. A
      // captured `state` from the click-time render can be stale by
      // dispatch time if a concurrent dismiss transitions the modal.
      // Functional setState updaters are not a substitute for a
      // synchronous read — React treats them as pure and may defer
      // or replay them.
      const current = stateRef.current;
      if (current.kind !== "manageEditEditingDetails") return;
      const id = current.site.id;
      savingRef.current = true;
      setSaving(true);
      await new Promise<void>((resolve) => {
        void updateSite({
          id,
          values,
          onSuccess: (site, warnings) => {
            pushToast({
              message:
                warnings.length > 0 ? `Site "${values.name}" saved with warnings` : `Site "${values.name}" saved`,
              status: STATUSES.success,
            });
            refetchSites();
            bumpSitesRevision();
            // The server regenerates the slug when the name changes. If the
            // renamed site is the active scope, sync the store — and the
            // current scoped URL — to the new slug. Otherwise the stale slug
            // makes ResolveSiteBySlug clear the selection on the next refresh
            // or "/" entry (the persisted entry path also goes through the
            // store, so updating it keeps appEntryPath correct).
            if (activeSite.kind === "site" && activeSite.id === site.id.toString() && activeSite.slug !== site.slug) {
              const next: ActiveSite = { kind: "site", id: activeSite.id, slug: site.slug };
              setActiveSite(next);
              if (routeScope?.kind === "site" && routeScope.id === site.id.toString()) {
                navigate(scopeCurrentOrDashboardPath(pathname, search, hash, next), { replace: true });
              }
            }
            // Functional setState so a mid-flight dismiss (state transition
            // back to manageEdit or none) can't be silently overwritten by a
            // stale onSuccess closure.
            setState((prev) =>
              prev.kind === "manageEditEditingDetails"
                ? { kind: "manageEdit", site, draft: siteFormValuesFromSite(site) }
                : prev,
            );
            resolve();
          },
          onError: (msg) => {
            pushToast({ message: `Failed to save site: ${msg}`, status: STATUSES.error });
            resolve();
          },
          onFinally: () => {
            savingRef.current = false;
            setSaving(false);
          },
        });
      });
    },
    [
      updateSite,
      refetchSites,
      bumpSitesRevision,
      activeSite,
      setActiveSite,
      routeScope,
      navigate,
      pathname,
      search,
      hash,
    ],
  );

  const manageEditDetails = useCallback(() => {
    setState((prev) => {
      // Stack details on top of manage. Manage stays in the underlying state
      // so it remains visible behind SiteSettingsModal.
      if (prev.kind === "manageEdit") {
        return { kind: "manageEditEditingDetails", site: prev.site, draft: prev.draft };
      }
      return prev;
    });
  }, []);

  // Inline building-create from the manage modal. The building is created and
  // associated to the current site in one transactional RPC (#821), so it
  // needs no follow-up AssignBuildingsToSite. Returns the created row to the
  // modal, which injects it into its local working set (rather than triggering
  // a list refetch) so any buildings staged via the picker are preserved.
  const manageCreateBuilding = useCallback(
    async (values: BuildingFormValues): Promise<Building | null> => {
      if (savingRef.current) return null;
      const site = managedSite(stateRef.current);
      if (!site) return null;
      const siteId = site.id;
      savingRef.current = true;
      setSaving(true);
      return await new Promise<Building | null>((resolve) => {
        void createBuilding({
          values,
          siteId,
          onSuccess: ({ building }) => {
            pushToast({ message: `Building "${building.name}" created`, status: STATUSES.success });
            // Refresh the site catalog (building counts) but deliberately NOT
            // the modal's building list — the modal injects the new row
            // locally so unsaved picker selections survive.
            refetchSites();
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
    [createBuilding, refetchSites],
  );

  // Inline bulk building-create. Same shape as manageCreateBuilding, but one
  // CreateBuildings call for the whole set: the transaction is all-or-nothing,
  // so the operator never has to work out which half of a prefix run exists.
  const manageCreateBuildings = useCallback(
    async (buildings: NewBuildingInput[]): Promise<BulkBuildingCreateResult> => {
      if (savingRef.current) return { created: [], errors: [] };
      const site = managedSite(stateRef.current);
      if (!site) return { created: [], errors: [] };
      const siteId = site.id;
      savingRef.current = true;
      setSaving(true);
      return await new Promise<BulkBuildingCreateResult>((resolve) => {
        void createBuildings({
          siteId,
          // Rows arrive fully formed from the modal: it owns the name
          // generation and the one layout the batch shares, so there is nothing
          // to reshape here.
          buildings,
          onSuccess: (buildings) => {
            pushToast({
              message: `${buildings.length} ${buildings.length === 1 ? "building" : "buildings"} created`,
              status: STATUSES.success,
            });
            // Site catalog only — the modal injects the new rows locally so
            // unsaved picker selections survive, same as single create.
            refetchSites();
            resolve({ created: buildings, errors: [] });
          },
          onError: (msg, errors) => {
            pushToast({ message: `Failed to create buildings: ${msg}`, status: STATUSES.error });
            resolve({ created: [], errors });
          },
          onFinally: () => {
            savingRef.current = false;
            setSaving(false);
          },
        });
      });
    },
    [createBuildings, refetchSites],
  );

  // Standalone-picker create. The manage* versions refresh only the site
  // catalog, leaving the building list to ManageSiteModal's local injection;
  // with no modal holding a working set, the host's list is the only thing that
  // renders these rows, so it has to be told.
  const pickerCreateBuilding = useCallback(
    async (values: BuildingFormValues): Promise<Building | null> => {
      const created = await manageCreateBuilding(values);
      if (created) refetchBuildings?.();
      return created;
    },
    [manageCreateBuilding, refetchBuildings],
  );

  const pickerCreateBuildings = useCallback(
    async (buildings: NewBuildingInput[]): Promise<BulkBuildingCreateResult> => {
      const result = await manageCreateBuildings(buildings);
      // All-or-nothing: an empty `created` means the transaction rolled back,
      // so refetching would only cost a round trip.
      if (result.created.length > 0) refetchBuildings?.();
      return result;
    },
    [manageCreateBuildings, refetchBuildings],
  );

  // Shared AssignBuildingsToSite dispatcher. `targetSiteId` unset moves the
  // buildings to "Unassigned"; either way the server cascades site_id down to
  // racks + devices.
  const dispatchAssign = useCallback(
    (buildingIds: bigint[], targetSiteId?: bigint) =>
      new Promise<void>((resolve, reject) => {
        if (buildingIds.length === 0) {
          resolve();
          return;
        }
        void assignBuildingsToSite({
          buildingIds,
          targetSiteId,
          onSuccess: () => resolve(),
          onError: (msg) => reject(new Error(msg)),
        });
      }),
    [assignBuildingsToSite],
  );

  const manageAssignBuildings = useCallback(
    async (delta: { added: bigint[]; removed: bigint[] }): Promise<boolean> => {
      if (savingRef.current) return false;
      const site = managedSite(stateRef.current);
      if (!site) return false;
      if (delta.added.length === 0 && delta.removed.length === 0) return true;
      const id = site.id;
      savingRef.current = true;
      setSaving(true);
      try {
        // Sequential so a mid-chain failure surfaces without a half-applied
        // success toast.
        await dispatchAssign(delta.removed, undefined);
        await dispatchAssign(delta.added, id);
      } catch (err) {
        const detail = err instanceof Error ? err.message : "Failed to assign buildings";
        pushToast({ message: `Failed to update buildings: ${detail}`, status: STATUSES.error });
        // The two calls aren't atomic across each other: the `removed` batch
        // may have already cascaded buildings out of the site before `added`
        // failed. Refresh so the counts + building table reflect what actually
        // committed rather than the now-stale pre-save view.
        refetchSites();
        refetchBuildings?.();
        return false;
      } finally {
        savingRef.current = false;
        setSaving(false);
      }
      const count = delta.added.length + delta.removed.length;
      pushToast({
        message: `${count} ${count === 1 ? "building" : "buildings"} updated`,
        status: STATUSES.success,
      });
      refetchSites();
      refetchBuildings?.();
      return true;
    },
    [dispatchAssign, refetchSites, refetchBuildings],
  );

  const manageRemoveBuilding = useCallback(
    async (buildingId: bigint, label: string): Promise<boolean> => {
      if (savingRef.current) return false;
      savingRef.current = true;
      setSaving(true);
      try {
        await dispatchAssign([buildingId], undefined);
      } catch (err) {
        const detail = err instanceof Error ? err.message : "Failed to remove building";
        pushToast({ message: `Failed to remove "${label}": ${detail}`, status: STATUSES.error });
        return false;
      } finally {
        savingRef.current = false;
        setSaving(false);
      }
      pushToast({ message: `"${label}" removed from this site`, status: STATUSES.success });
      refetchSites();
      refetchBuildings?.();
      return true;
    },
    [dispatchAssign, refetchSites, refetchBuildings],
  );

  const deleteConfirm = useCallback(async () => {
    if (!deleteTarget) return;
    const id = deleteTarget.site?.id;
    const name = deleteTarget.site?.name ?? "site";
    if (!id || id === 0n) return;

    setDeleting(true);
    await new Promise<void>((resolve) => {
      void deleteSite({
        id,
        onSuccess: () => {
          pushToast({ message: `Site "${name}" deleted`, status: STATUSES.success });
          // Reset the active SitePicker selection explicitly when the deleted
          // site was the active one. The useActiveSite reset effect bails
          // when knownSiteIds is empty, so a failed refetch could otherwise
          // leak a stale active-site id into the persisted Zustand store.
          if (activeSite.kind === "site" && activeSite.id === id.toString()) {
            setActiveSite({ kind: "all" });
          }
          refetchSites();
          bumpSitesRevision();
          setDeleteTarget(null);
          // Edit-flow callers come from manageEditEditingDetails or
          // manageEdit; the deleted site is gone so we collapse the stack.
          setState({ kind: "none" });
          resolve();
        },
        onError: (msg) => {
          pushToast({ message: `Failed to delete site: ${msg}`, status: STATUSES.error });
          resolve();
        },
        onFinally: () => setDeleting(false),
      });
    });
  }, [deleteTarget, deleteSite, refetchSites, activeSite, setActiveSite, bumpSitesRevision]);

  return {
    state,
    deleteTarget,
    saving,
    deleting,
    manageUnassignedRackCount,
    manageUnassignedMinerCount,
    openCreate,
    openManageEdit,
    openBuildingsPicker,
    requestDeleteCurrent,
    dismiss,
    dismissDeleteConfirm,
    detailsContinueCreate,
    detailsSaveEdit,
    manageEditDetails,
    manageCreateBuilding,
    manageCreateBuildings,
    pickerCreateBuilding,
    pickerCreateBuildings,
    manageAssignBuildings,
    manageRemoveBuilding,
    deleteConfirm,
  };
};

export { useSiteModals };
