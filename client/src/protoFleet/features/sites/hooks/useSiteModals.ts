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
  openManageEdit: (site: Site) => void;
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
  manageNetworkConfigChange: (value: string) => void;
  manageSave: () => Promise<{
    canonicalNetworkConfig: string;
    warnings: string[];
    closeOnSuccess: boolean;
  } | null>;
  // SiteDeleteDialog handlers
  deleteConfirm: () => Promise<void>;
}

const useSiteModals = ({ refetchSites }: UseSiteModalsOptions): SiteModalsApi => {
  const [state, setState] = useState<SiteModalState>({ kind: "none" });
  const [deleteTarget, setDeleteTarget] = useState<SiteWithCounts | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

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

  const { createSite, updateSite, deleteSite } = useSites();
  const setActiveSite = useFleetStore((store) => store.ui.setActiveSite);
  const activeSite = useFleetStore((store) => store.ui.activeSite);

  const openCreate = useCallback(() => {
    setState({ kind: "detailsCreate", draft: emptySiteFormValues() });
  }, []);

  const openManageEdit = useCallback((site: Site) => {
    setState({ kind: "manageEdit", site, draft: siteFormValuesFromSite(site) });
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
    [updateSite, refetchSites],
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

  const manageNetworkConfigChange = useCallback((value: string) => {
    setState((prev) => {
      if (prev.kind === "manageCreate" || prev.kind === "manageCreateEditingDetails") {
        return { ...prev, draft: { ...prev.draft, networkConfig: value } };
      }
      if (prev.kind === "manageEdit" || prev.kind === "manageEditEditingDetails") {
        return { ...prev, draft: { ...prev.draft, networkConfig: value } };
      }
      return prev;
    });
  }, []);

  const manageSave = useCallback(async () => {
    if (savingRef.current) return null;

    if (state.kind === "manageCreate") {
      const draft = state.draft;
      savingRef.current = true;
      setSaving(true);
      const result = await new Promise<{
        canonicalNetworkConfig: string;
        warnings: string[];
        closeOnSuccess: boolean;
      } | null>((resolve) => {
        void createSite({
          values: draft,
          onSuccess: (site, warnings) => {
            pushToast({
              message:
                warnings.length > 0 ? `Site "${site.name}" created with warnings` : `Site "${site.name}" created`,
              status: STATUSES.success,
            });
            refetchSites();
            // Transition to manageEdit so a follow-up Save calls UpdateSite
            // (idempotent) instead of re-running CreateSite with the same
            // payload and producing a duplicate site row.
            setState({ kind: "manageEdit", site, draft: siteFormValuesFromSite(site) });
            resolve({
              canonicalNetworkConfig: site.networkConfig,
              warnings,
              // When warnings are present we keep the modal open so the
              // operator can review the canonical text + warning copy. The
              // state has flipped to manageEdit, so any follow-up Save runs
              // UpdateSite — safe.
              closeOnSuccess: warnings.length === 0,
            });
          },
          onError: (msg) => {
            pushToast({ message: `Failed to create site: ${msg}`, status: STATUSES.error });
            resolve(null);
          },
          onFinally: () => {
            savingRef.current = false;
            setSaving(false);
          },
        });
        // TODO(phase-1b #199): once the miner picker ships, follow this
        // CreateSite call with `assignDevicesToSite({ targetSiteId: site.id,
        // deviceIdentifiers: pendingDeviceIds })` inside the onSuccess branch
        // (and gate setSaving(false) on the inner onFinally). Partial-failure
        // toast wording is already drafted in PR #292 review.
      });
      return result;
    }

    if (state.kind === "manageEdit") {
      const draft = state.draft;
      const id = state.site.id;
      savingRef.current = true;
      setSaving(true);
      const result = await new Promise<{
        canonicalNetworkConfig: string;
        warnings: string[];
        closeOnSuccess: boolean;
      } | null>((resolve) => {
        void updateSite({
          id,
          values: draft,
          onSuccess: (site, warnings) => {
            pushToast({
              message: warnings.length > 0 ? `Site "${site.name}" saved with warnings` : `Site "${site.name}" saved`,
              status: STATUSES.success,
            });
            refetchSites();
            // Refresh the draft so the operator sees the server's canonical
            // values reflected in the manage preview.
            setState((prev) =>
              prev.kind === "manageEdit" ? { kind: "manageEdit", site, draft: siteFormValuesFromSite(site) } : prev,
            );
            resolve({
              canonicalNetworkConfig: site.networkConfig,
              warnings,
              closeOnSuccess: warnings.length === 0,
            });
          },
          onError: (msg) => {
            pushToast({ message: `Failed to save site: ${msg}`, status: STATUSES.error });
            resolve(null);
          },
          onFinally: () => {
            savingRef.current = false;
            setSaving(false);
          },
        });
      });
      return result;
    }

    return null;
  }, [state, createSite, updateSite, refetchSites]);

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
  }, [deleteTarget, deleteSite, refetchSites, activeSite, setActiveSite]);

  return {
    state,
    deleteTarget,
    saving,
    deleting,
    openCreate,
    openManageEdit,
    requestDeleteCurrent,
    dismiss,
    cancelAll,
    dismissDeleteConfirm,
    detailsContinueCreate,
    detailsSaveEdit,
    manageEditDetails,
    manageNetworkConfigChange,
    manageSave,
    deleteConfirm,
  };
};

export { useSiteModals };
