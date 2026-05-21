import { useCallback, useState } from "react";

import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues, type SiteFormValues, siteFormValuesFromSite, useSites } from "@/protoFleet/api/sites";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// State machine for the site CRUD modal flow. Lifted out of the page
// components so /sites and /settings/sites share the exact same wiring;
// they differ only on the render side.
export type SiteModalState =
  | { kind: "none" }
  | { kind: "detailsCreate"; draft: SiteFormValues }
  | { kind: "manageCreate"; draft: SiteFormValues }
  // Stacked: ManageSiteModal stays open while SiteDetailsModal renders
  // on top. CTAs in details read Delete (discard pending create) + Save
  // (apply changes and return to manage).
  | { kind: "manageCreateEditingDetails"; draft: SiteFormValues }
  | { kind: "manageEdit"; site: Site; draft: SiteFormValues }
  // Stacked edit-flow counterpart. Save calls UpdateSite directly; on
  // success details closes and manage stays open with refreshed draft.
  | { kind: "manageEditEditingDetails"; site: Site; draft: SiteFormValues }
  | { kind: "deleteConfirm"; site: SiteWithCounts };

interface UseSiteModalsOptions {
  // Parent refetches sites after every successful mutation. Buildings are
  // refetched on demand inside ManageSiteModal so we don't need a hook for
  // them here.
  refetchSites: () => void;
}

export interface SiteModalsApi {
  state: SiteModalState;
  saving: boolean;
  deleting: boolean;
  openCreate: () => void;
  openManageEdit: (site: Site) => void;
  openDeleteConfirm: (site: SiteWithCounts) => void;
  // Closes the topmost modal: drops details if details is stacked on
  // manage, otherwise closes everything to none.
  dismiss: () => void;
  // Closes every modal regardless of stack — used when the operator
  // discards a pending create from the SiteDetailsModal Delete button.
  cancelAll: () => void;
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

// The two-call orchestration described in master plan §J3 wraps
// CreateSite + an optional ReassignDevicesToSite. The miner picker that
// populates pendingDeviceIds lands in Phase 1b (#199); until then this
// stays empty and the second call short-circuits. The plumbing is here
// so the picker only needs to flip a setter when it ships.
const PHASE_1B_PENDING_DEVICE_IDS: string[] = [];

const useSiteModals = ({ refetchSites }: UseSiteModalsOptions): SiteModalsApi => {
  const [state, setState] = useState<SiteModalState>({ kind: "none" });
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const { createSite, updateSite, deleteSite, reassignDevicesToSite } = useSites();

  const openCreate = useCallback(() => {
    setState({ kind: "detailsCreate", draft: emptySiteFormValues() });
  }, []);

  const openManageEdit = useCallback((site: Site) => {
    setState({ kind: "manageEdit", site, draft: siteFormValuesFromSite(site) });
  }, []);

  const openDeleteConfirm = useCallback((site: SiteWithCounts) => {
    setState({ kind: "deleteConfirm", site });
  }, []);

  const dismiss = useCallback(() => {
    // Stacked states drop just the top (details) and return to the
    // underlying manage state. Everything else closes to none.
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
      if (state.kind !== "manageEditEditingDetails") return;
      const id = state.site.id;
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
            // Drop details, keep ManageSiteModal open with the server's
            // canonical site + draft so the operator sees the saved state
            // reflected in the manage preview.
            setState({ kind: "manageEdit", site, draft: siteFormValuesFromSite(site) });
            resolve();
          },
          onError: (msg) => {
            pushToast({ message: `Failed to save site: ${msg}`, status: STATUSES.error });
            resolve();
          },
          onFinally: () => setSaving(false),
        });
      });
    },
    [state, updateSite, refetchSites],
  );

  const manageEditDetails = useCallback(() => {
    setState((prev) => {
      // Stack details on top of manage. Manage stays in the underlying
      // state so it remains visible behind SiteDetailsModal.
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
    if (state.kind === "manageCreate") {
      const draft = state.draft;
      setSaving(true);
      const result = await new Promise<{
        canonicalNetworkConfig: string;
        warnings: string[];
        closeOnSuccess: boolean;
      } | null>((resolve) => {
        void createSite({
          values: draft,
          onSuccess: (site, warnings) => {
            // Two-call orchestration (master plan §J3): only fires when a
            // miner picker (Phase 1b) populates pendingDeviceIds. Today the
            // list is always empty, so we skip the second call.
            const pendingDeviceIds = PHASE_1B_PENDING_DEVICE_IDS;
            if (pendingDeviceIds.length === 0) {
              pushToast({
                message:
                  warnings.length > 0 ? `Site "${site.name}" created with warnings` : `Site "${site.name}" created`,
                status: STATUSES.success,
              });
              refetchSites();
              resolve({
                canonicalNetworkConfig: site.networkConfig,
                warnings,
                // Block close-on-success when the server returned warnings so
                // the operator can review the canonical text + warning copy
                // before dismissing. A second Save with no further edits
                // closes the modal (idempotent UpdateSite would be cleaner
                // here, but for create flow the row already exists, so the
                // operator must explicitly dismiss).
                closeOnSuccess: warnings.length === 0,
              });
              return;
            }

            // Phase 1b miner picker will populate `pendingDeviceIds`. When
            // ReassignDevicesToSite fails the site row stays — operator
            // recovers via the miner list per plan §J3.
            void reassignDevicesToSite({
              targetSiteId: site.id,
              deviceIdentifiers: pendingDeviceIds,
              onSuccess: () => {
                pushToast({
                  message: `Site "${site.name}" created`,
                  status: STATUSES.success,
                });
                refetchSites();
                resolve({
                  canonicalNetworkConfig: site.networkConfig,
                  warnings,
                  closeOnSuccess: warnings.length === 0,
                });
              },
              onError: (msg) => {
                pushToast({
                  message: `Site "${site.name}" created; miner assignment failed: ${msg}`,
                  status: STATUSES.error,
                });
                refetchSites();
                resolve({
                  canonicalNetworkConfig: site.networkConfig,
                  warnings,
                  closeOnSuccess: true,
                });
              },
            });
          },
          onError: (msg) => {
            pushToast({ message: `Failed to create site: ${msg}`, status: STATUSES.error });
            resolve(null);
          },
          onFinally: () => setSaving(false),
        });
      });
      return result;
    }

    if (state.kind === "manageEdit") {
      const draft = state.draft;
      const id = state.site.id;
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
          onFinally: () => setSaving(false),
        });
      });
      return result;
    }

    return null;
  }, [state, createSite, updateSite, reassignDevicesToSite, refetchSites]);

  const deleteConfirm = useCallback(async () => {
    if (state.kind !== "deleteConfirm") return;
    const id = state.site.site?.id;
    const name = state.site.site?.name ?? "site";
    if (!id || id === 0n) return;

    setDeleting(true);
    await new Promise<void>((resolve) => {
      void deleteSite({
        id,
        onSuccess: () => {
          pushToast({ message: `Site "${name}" deleted`, status: STATUSES.success });
          // useActiveSite's validation effect resets the picker to "all"
          // automatically once the refetch removes the deleted id from
          // knownSiteIds — no explicit setActiveSite call needed here.
          refetchSites();
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
  }, [state, deleteSite, refetchSites]);

  return {
    state,
    saving,
    deleting,
    openCreate,
    openManageEdit,
    openDeleteConfirm,
    dismiss,
    cancelAll,
    detailsContinueCreate,
    detailsSaveEdit,
    manageEditDetails,
    manageNetworkConfigChange,
    manageSave,
    deleteConfirm,
  };
};

export { useSiteModals };
