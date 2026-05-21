import { useCallback, useRef, useState } from "react";

import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues, type SiteFormValues, siteFormValuesFromSite, useSites } from "@/protoFleet/api/sites";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Modal-stack state. deleteConfirm lives in a parallel field (not this union)
// so the cascade dialog renders as a sibling that overlays the stacked
// manage/details modals without unmounting them — mirroring AssignMinersModal's
// pattern. Cancel on the cascade dialog returns the operator to whichever
// modal they came from.
export type SiteModalState =
  | { kind: "none" }
  | { kind: "detailsCreate"; draft: SiteFormValues }
  | { kind: "manageCreate"; draft: SiteFormValues }
  // Stacked: ManageSiteModal stays open while SiteDetailsModal renders on
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
  // cascade dialog. The hook does the lookup so /sites and /settings/sites
  // don't duplicate the same id-matching logic.
  requestDeleteCurrent: (sites: SiteWithCounts[] | undefined) => void;
  // Closes the topmost modal: drops details if details is stacked on
  // manage, otherwise closes everything to none.
  dismiss: () => void;
  // Closes every modal regardless of stack — used when the operator
  // discards a pending create from the SiteDetailsModal Delete button.
  cancelAll: () => void;
  // SiteDeleteDialog onDismiss — closes only the cascade dialog.
  dismissDeleteConfirm: () => void;
  // SiteDetailsModal handlers
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
    // clicked inside SiteDetailsModal (edit mode) or any future row-level
    // delete affordance.
    setState((prev) => {
      const id =
        prev.kind === "manageEdit" || prev.kind === "manageEditEditingDetails" ? prev.site.id.toString() : null;
      if (!id) return prev;
      const match = sites?.find((s) => (s.site?.id ?? 0n).toString() === id);
      if (match) setDeleteTarget(match);
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
    // Carry the existing networkConfig draft through; SiteDetailsModal only
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
      if (state.kind !== "manageEditEditingDetails") return;
      const id = state.site.id;
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
    [state, updateSite, refetchSites],
  );

  const manageEditDetails = useCallback(() => {
    setState((prev) => {
      // Stack details on top of manage. Manage stays in the underlying state
      // so it remains visible behind SiteDetailsModal.
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
        // CreateSite call with `reassignDevicesToSite({ targetSiteId: site.id,
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
