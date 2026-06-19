import ManageSiteModal from "../ManageSiteModal";
import SiteDeleteDialog from "../SiteDeleteDialog";
import SiteSettingsModal from "../SiteSettingsModal";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";

interface SiteModalsProps {
  modals: ReturnType<typeof useSiteModals>;
  // SiteWithCounts cache from the host page. Used to resolve the cascade
  // dialog target when Delete is clicked from ManageSiteModal or
  // SiteSettingsModal (edit mode).
  sites: SiteWithCounts[] | undefined;
  // Refresh signal for the manage modal's building list — bumped by the
  // host's useBuildingModals on any building mutation made elsewhere.
  buildingsRefreshKey?: number;
}

const SiteModals = ({ modals, sites, buildingsRefreshKey }: SiteModalsProps) => {
  const { state, deleteTarget } = modals;
  const showManage =
    state.kind === "manageCreate" ||
    state.kind === "manageEdit" ||
    state.kind === "manageCreateEditingDetails" ||
    state.kind === "manageEditEditingDetails";

  // Delete in the create flow (whether or not details is stacked) discards
  // the pending create; edit-flow routes through requestDeleteCurrent which
  // opens the cascade dialog from the page-level cache.
  const handleDelete = () => {
    if (state.kind === "manageCreate" || state.kind === "manageCreateEditingDetails") {
      modals.cancelAll();
      return;
    }
    modals.requestDeleteCurrent(sites);
  };

  const manageDraft = showManage ? state.draft : undefined;
  const manageSite = state.kind === "manageEdit" || state.kind === "manageEditEditingDetails" ? state.site : undefined;
  const manageMode = state.kind === "manageEdit" || state.kind === "manageEditEditingDetails" ? "edit" : "create";

  return (
    <>
      {/* Render ManageSiteModal first so SiteSettingsModal's portal lands
          later in the DOM and naturally stacks on top at the same z-50. */}
      {showManage && manageDraft ? (
        <ManageSiteModal
          // Key on the site id so switching directly between sites (or
          // create → edit) remounts the modal with a fresh building working
          // set + load-time snapshot, instead of briefly rendering the prior
          // site's entries until the new fetch resolves. Mirrors how the host
          // keys ManageBuildingModal on building.id.
          key={manageSite ? manageSite.id.toString() : "create"}
          open
          mode={manageMode}
          draft={manageDraft}
          site={manageSite}
          onSave={modals.manageSave}
          onEditDetails={modals.manageEditDetails}
          onDeleteRequested={handleDelete}
          onDismiss={modals.dismiss}
          saving={modals.saving}
          buildingsRefreshKey={buildingsRefreshKey}
        />
      ) : null}
      {state.kind === "detailsCreate" ? (
        <SiteSettingsModal
          open
          mode="create"
          initialValues={state.draft}
          onContinue={modals.detailsContinueCreate}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {state.kind === "manageCreateEditingDetails" ? (
        <SiteSettingsModal
          open
          mode="createReturn"
          initialValues={state.draft}
          onContinue={modals.detailsContinueCreate}
          onDeleteRequested={handleDelete}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {state.kind === "manageEditEditingDetails" ? (
        <SiteSettingsModal
          open
          mode="edit"
          initialValues={state.draft}
          onSave={modals.detailsSaveEdit}
          onDeleteRequested={handleDelete}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {/* SiteDeleteDialog renders as a sibling — overlays whichever modal is
          underneath without unmounting it. Cancel returns to the prior
          context (manage / details / page) instead of collapsing the stack. */}
      {deleteTarget ? (
        <SiteDeleteDialog
          open
          site={deleteTarget}
          onConfirm={modals.deleteConfirm}
          onDismiss={modals.dismissDeleteConfirm}
          deleting={modals.deleting}
        />
      ) : null}
    </>
  );
};

export default SiteModals;
