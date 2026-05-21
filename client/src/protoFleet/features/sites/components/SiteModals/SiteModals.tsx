import ManageSiteModal from "../ManageSiteModal";
import SiteDeleteDialog from "../SiteDeleteDialog";
import SiteDetailsModal, { type SiteDetailsModalMode } from "../SiteDetailsModal";
import { emptySiteFormValues } from "@/protoFleet/api/sites";
import { type useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";

interface SiteModalsProps {
  modals: ReturnType<typeof useSiteModals>;
  // Pages own the SiteWithCounts cache, so they're responsible for
  // resolving the Site → SiteWithCounts row needed to drive the cascade
  // dialog. The createReturn case never reaches this handler — Delete
  // there means "discard pending create" and is wired to dismiss directly.
  onDeleteFromDetailsEdit: () => void;
}

const detailsModeFor = (kind: string): SiteDetailsModalMode => {
  if (kind === "detailsEdit") return "edit";
  if (kind === "detailsCreateReturn") return "createReturn";
  return "create";
};

const SiteModals = ({ modals, onDeleteFromDetailsEdit }: SiteModalsProps) => {
  const { state } = modals;
  const inDetails =
    state.kind === "detailsCreate" || state.kind === "detailsEdit" || state.kind === "detailsCreateReturn";
  const inManage = state.kind === "manageCreate" || state.kind === "manageEdit";

  // Delete in createReturn discards the in-progress create; in detailsEdit
  // it opens the cascade dialog from the page-level cache.
  const handleDelete = () => {
    if (state.kind === "detailsCreateReturn") {
      modals.dismiss();
      return;
    }
    onDeleteFromDetailsEdit();
  };

  return (
    <>
      <SiteDetailsModal
        open={inDetails}
        mode={detailsModeFor(state.kind)}
        initialValues={inDetails ? state.draft : emptySiteFormValues()}
        onContinue={modals.detailsContinueCreate}
        onSave={modals.detailsSaveEdit}
        onDeleteRequested={handleDelete}
        onDismiss={modals.dismiss}
        saving={modals.saving}
      />
      <ManageSiteModal
        open={inManage}
        mode={state.kind === "manageEdit" ? "edit" : "create"}
        draft={inManage ? state.draft : emptySiteFormValues()}
        site={state.kind === "manageEdit" ? state.site : undefined}
        onSave={modals.manageSave}
        onEditDetails={modals.manageEditDetails}
        onNetworkConfigChange={modals.manageNetworkConfigChange}
        onDismiss={modals.dismiss}
        saving={modals.saving}
      />
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
