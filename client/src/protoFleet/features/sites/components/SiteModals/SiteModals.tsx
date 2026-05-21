import ManageSiteModal from "../ManageSiteModal";
import SiteDeleteDialog from "../SiteDeleteDialog";
import SiteDetailsModal, { type SiteDetailsModalMode } from "../SiteDetailsModal";
import { type useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";

interface SiteModalsProps {
  modals: ReturnType<typeof useSiteModals>;
  // Pages own the SiteWithCounts cache, so they're responsible for
  // resolving the Site → SiteWithCounts row needed to drive the cascade
  // dialog. The create-flow stacked state never reaches this handler —
  // Delete there means "discard pending create" and dismisses directly.
  onDeleteFromDetailsEdit: () => void;
}

const detailsModeFor = (kind: string): SiteDetailsModalMode => {
  if (kind === "manageEditEditingDetails") return "edit";
  if (kind === "manageCreateEditingDetails") return "createReturn";
  return "create";
};

const SiteModals = ({ modals, onDeleteFromDetailsEdit }: SiteModalsProps) => {
  const { state } = modals;
  const showDetails =
    state.kind === "detailsCreate" ||
    state.kind === "manageCreateEditingDetails" ||
    state.kind === "manageEditEditingDetails";
  const showManage =
    state.kind === "manageCreate" ||
    state.kind === "manageEdit" ||
    state.kind === "manageCreateEditingDetails" ||
    state.kind === "manageEditEditingDetails";

  // Delete in create-flow stacked state discards the pending create entirely;
  // edit-flow stacked state opens the cascade dialog via the page-level cache.
  const handleDelete = () => {
    if (state.kind === "manageCreateEditingDetails") {
      modals.cancelAll();
      return;
    }
    onDeleteFromDetailsEdit();
  };

  // ManageSiteModal data: the underlying manage state owns site + draft. In
  // the stacked variants the underlying manage state is implied (manageCreate
  // sits under manageCreateEditingDetails, manageEdit under
  // manageEditEditingDetails) so we read from state.draft / state.site.
  const manageDraft = showManage ? state.draft : undefined;
  const manageSite = state.kind === "manageEdit" || state.kind === "manageEditEditingDetails" ? state.site : undefined;
  const manageMode = state.kind === "manageEdit" || state.kind === "manageEditEditingDetails" ? "edit" : "create";

  return (
    <>
      {/* Render ManageSiteModal first so SiteDetailsModal's portal lands
          later in the DOM and naturally stacks on top at the same z-50. */}
      {showManage && manageDraft ? (
        <ManageSiteModal
          open
          mode={manageMode}
          draft={manageDraft}
          site={manageSite}
          onSave={modals.manageSave}
          onEditDetails={modals.manageEditDetails}
          onNetworkConfigChange={modals.manageNetworkConfigChange}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {showDetails ? (
        <SiteDetailsModal
          open
          mode={detailsModeFor(state.kind)}
          initialValues={state.draft}
          onContinue={modals.detailsContinueCreate}
          onSave={modals.detailsSaveEdit}
          onDeleteRequested={handleDelete}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {state.kind === "deleteConfirm" ? (
        <SiteDeleteDialog
          open
          site={state.site}
          onConfirm={modals.deleteConfirm}
          onDismiss={modals.dismiss}
          deleting={modals.deleting}
        />
      ) : null}
    </>
  );
};

export default SiteModals;
