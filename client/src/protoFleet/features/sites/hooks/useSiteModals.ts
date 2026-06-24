import { useCallback, useEffect, useRef, useState } from "react";

import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues, type SiteFormValues, siteFormValuesFromSite, useSites } from "@/protoFleet/api/sites";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Modal-stack state. deleteConfirm lives in a parallel field (not this union)
// so the cascade dialog renders as a sibling that overlays the stacked
// manage/details modals without unmounting them — mirroring ManageRackModal's
// pattern. Cancel on the cascade dialog returns the operator to whichever
// modal they came from.
export type SiteModalState =
  | { kind: "none" }
  | { kind: "detailsCreate"; draft: SiteFormValues }
  | { kind: "manageCreate"; draft: SiteFormValues }
  // Stacked: ManageSiteModal stays open while SiteSettingsModal renders on
  // top. CTAs in details read Delete (discard pending create) + Save (apply
  // changes and return to manage).
  | { kind: "manageCreateEditingDetails"; draft: SiteFormValues }
  | { kind: "manageEdit"; site: Site; draft: SiteFormValues }
  // Stacked edit-flow counterpart. Save calls UpdateSite directly; on
  // success details closes and manage stays open with refreshed draft.
  | { kind: "manageEditEditingDetails"; site: Site; draft: SiteFormValues };

interface UseSiteModalsOptions {
  refetchSites: () => void;
  // Bumps the page's building-refresh signal so any sibling building list
  // (e.g. SiteSettingsSingleView's table) re-fetches after a membership
  // change. Optional so hosts without a building table can omit it.
  refetchBuildings?: () => void;
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
  manageUnassignedRackCount: number | undefined;
  manageUnassignedMinerCount: number | undefined;
  // Resolve a SiteWithCounts from the page's sites cache and open the
  // cascade dialog. The hook does the lookup so callers don't duplicate the
  // same id-matching logic.
  requestDeleteCurrent: (sites: SiteWithCounts[] | undefined) => void;
  // Closes the topmost modal: drops details if details is stacked on
  // manage, otherwise closes everything to none.
  dismiss: () => void;
  // Closes every modal regardless of stack — used when the operator
  // discards a pending create from the SiteSettingsModal Delete button.
  cancelAll: () => void;
  // SiteDeleteDialog onDismiss — closes only the cascade dialog.
  dismissDeleteConfirm: () => void;
  // SiteSettingsModal handlers
  detailsContinueCreate: (values: SiteFormValues) => void;
  detailsSaveEdit: (values: SiteFormValues) => Promise<void>;
  // ManageSiteModal handlers
  manageEditDetails: () => void;
  // Persists building-membership changes accumulated in the manage modal.
  // In create mode this runs CreateSite, then assigns any buildings the
  // operator staged (the delta's `added`) to the new site. In edit mode it
  // applies the delta via AssignBuildingsToSite. Returns whether the modal
  // should close on success, or null if the save failed.
  manageSave: (delta: { added: bigint[]; removed: bigint[] }) => Promise<{ closeOnSuccess: boolean } | null>;
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
  const setActiveSite = useFleetStore((store) => store.ui.setActiveSite);
  const activeSite = useFleetStore((store) => store.ui.activeSite);
  // Signals the PageHeader's SitePicker (which fetches sites once on mount and
  // can't see this page's refetchSites) to refresh after a site is created,
  // renamed, or deleted.
  const bumpSitesRevision = useFleetStore((store) => store.ui.bumpSitesRevision);

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
    // Stacked states drop just the top (details) and return to the underlying
    // manage state. Everything else closes to none.
    setState((prev) => {
      if (prev.kind === "manageCreateEditingDetails") return { kind: "manageCreate", draft: prev.draft };
      if (prev.kind === "manageEditEditingDetails") {
        return { kind: "manageEdit", site: prev.site, draft: prev.draft };
      }
      return { kind: "none" };
    });
  }, []);

  const cancelAll = useCallback(() => {
    setState({ kind: "none" });
    setDeleteTarget(null);
  }, []);

  const dismissDeleteConfirm = useCallback(() => {
    setDeleteTarget(null);
  }, []);

  const detailsContinueCreate = useCallback((values: SiteFormValues) => {
    // Carry the existing networkConfig draft through; SiteSettingsModal only
    // owns the descriptive fields, so the value typed in ManageSiteModal
    // survives bouncing between the two surfaces.
    setState((prev) => {
      if (prev.kind === "detailsCreate" || prev.kind === "manageCreateEditingDetails") {
        return { kind: "manageCreate", draft: { ...values, networkConfig: prev.draft.networkConfig } };
      }
      return prev;
    });
  }, []);

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
    [updateSite, refetchSites, bumpSitesRevision],
  );

  const manageEditDetails = useCallback(() => {
    setState((prev) => {
      // Stack details on top of manage. Manage stays in the underlying state
      // so it remains visible behind SiteSettingsModal.
      if (prev.kind === "manageCreate") return { kind: "manageCreateEditingDetails", draft: prev.draft };
      if (prev.kind === "manageEdit") {
        return { kind: "manageEditEditingDetails", site: prev.site, draft: prev.draft };
      }
      return prev;
    });
  }, []);

  const manageSave = useCallback(
    async (delta: { added: bigint[]; removed: bigint[] }) => {
      if (savingRef.current) return null;

      // Create flow: persist the site, then assign any buildings the operator
      // staged in the manage modal before the site existed. `added` carries
      // those buildings (the working set started empty, so there's never a
      // `removed`); they're assigned to the freshly-created site's id. The two
      // steps are sequenced explicitly (rather than chained through
      // createSite's onFinally) so the saving guard stays held across both —
      // an async onSuccess would otherwise release it the moment the building
      // assign awaited.
      if (state.kind === "manageCreate") {
        const draft = state.draft;
        savingRef.current = true;
        setSaving(true);
        try {
          const created = await new Promise<{ site: Site; warnings: string[] } | null>((resolve) => {
            void createSite({
              values: draft,
              onSuccess: (site, warnings) => resolve({ site, warnings }),
              onError: (msg) => {
                pushToast({ message: `Failed to create site: ${msg}`, status: STATUSES.error });
                resolve(null);
              },
            });
          });
          if (!created) return null;

          // The site is committed past this point. A subsequent
          // AssignBuildingsToSite failure must not read as a failed create —
          // surface a partial-success warning and still close, matching the
          // bulk "New site" seeded flow in FleetCreateFlowProvider.
          let buildingsFailed: string | null = null;
          if (delta.added.length > 0) {
            await new Promise<void>((resolve) => {
              void assignBuildingsToSite({
                buildingIds: delta.added,
                targetSiteId: created.site.id,
                onSuccess: () => resolve(),
                onError: (msg) => {
                  buildingsFailed = msg;
                  resolve();
                },
              });
            });
          }

          pushToast(
            buildingsFailed
              ? {
                  message: `Site "${created.site.name}" created, but adding buildings failed: ${buildingsFailed}`,
                  status: STATUSES.error,
                }
              : {
                  message:
                    created.warnings.length > 0
                      ? `Site "${created.site.name}" created with warnings`
                      : `Site "${created.site.name}" created`,
                  status: STATUSES.success,
                },
          );
          refetchSites();
          refetchBuildings?.();
          bumpSitesRevision();
          return { closeOnSuccess: true };
        } finally {
          savingRef.current = false;
          setSaving(false);
        }
      }

      // Edit flow: site details are owned by SiteSettingsModal, so the
      // manage modal's Save only persists building membership. A no-op
      // delta (operator opened Save without changes) closes silently.
      if (state.kind === "manageEdit") {
        if (delta.added.length === 0 && delta.removed.length === 0) {
          return { closeOnSuccess: true };
        }
        const id = state.site.id;
        const name = state.site.name;
        savingRef.current = true;
        setSaving(true);
        try {
          // `added` moves buildings into this site; `removed` moves them to
          // "Unassigned" (targetSiteId unset). Both cascade site_id down to
          // racks + devices server-side. Run sequentially so a mid-chain
          // failure surfaces without a half-applied toast.
          const dispatch = (buildingIds: bigint[], targetSiteId?: bigint) =>
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
            });
          try {
            await dispatch(delta.removed, undefined);
            await dispatch(delta.added, id);
          } catch (err) {
            const detail = err instanceof Error ? err.message : "Failed to save buildings";
            pushToast({ message: `Failed to save site: ${detail}`, status: STATUSES.error });
            // The two AssignBuildingsToSite calls aren't atomic across each
            // other: the `removed` batch may have already cascaded buildings
            // out of the site before the `added` batch failed. Refresh so the
            // counts + building table reflect what actually committed rather
            // than the now-stale pre-save view.
            refetchSites();
            refetchBuildings?.();
            return null;
          }
          pushToast({ message: `Site "${name}" saved`, status: STATUSES.success });
          refetchSites();
          refetchBuildings?.();
          return { closeOnSuccess: true };
        } finally {
          savingRef.current = false;
          setSaving(false);
        }
      }

      return null;
    },
    [state, createSite, assignBuildingsToSite, refetchSites, refetchBuildings, bumpSitesRevision],
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
    requestDeleteCurrent,
    dismiss,
    cancelAll,
    dismissDeleteConfirm,
    detailsContinueCreate,
    detailsSaveEdit,
    manageEditDetails,
    manageSave,
    deleteConfirm,
  };
};

export { useSiteModals };
