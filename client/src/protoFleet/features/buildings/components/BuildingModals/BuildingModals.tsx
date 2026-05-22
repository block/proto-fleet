import BuildingDeleteDialog from "../BuildingDeleteDialog";
import BuildingDetailsModal from "../BuildingDetailsModal";
import ManageBuildingModal from "../ManageBuildingModal";
import { type useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";

interface BuildingModalsProps {
  modals: ReturnType<typeof useBuildingModals>;
}

// Renders whichever building modal is on top of the stack. The modals hook
// owns BuildingWithCounts in its edit-bearing states, so this host needs no
// external buildings cache to resolve cascade-dialog rack_count.
const BuildingModals = ({ modals }: BuildingModalsProps) => {
  const { state, deleteTarget } = modals;
  const showManage = state.kind === "manage" || state.kind === "manageEditingDetails";

  return (
    <>
      {/* ManageBuildingModal renders first so BuildingDetailsModal's portal
          lands later in the DOM and naturally stacks on top. */}
      {showManage && state.row.building ? (
        <ManageBuildingModal
          open
          building={state.row.building}
          siteName={state.siteName}
          onDismiss={modals.dismiss}
          onEditDetails={modals.manageEditDetails}
        />
      ) : null}
      {state.kind === "detailsCreate" ? (
        <BuildingDetailsModal
          open
          mode="create"
          initialValues={state.draft}
          parentSiteLabel={state.siteName}
          onSave={async (values) => {
            await modals.detailsCreate(values);
          }}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {state.kind === "detailsEdit" ? (
        <BuildingDetailsModal
          open
          mode="edit"
          initialValues={state.draft}
          parentSiteLabel={state.siteName}
          onSave={async (values) => {
            await modals.detailsSaveEdit(values);
          }}
          onDeleteRequested={modals.requestDeleteCurrent}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {state.kind === "manageEditingDetails" ? (
        <BuildingDetailsModal
          open
          mode="edit"
          initialValues={state.draft}
          parentSiteLabel={state.siteName}
          onSave={async (values) => {
            await modals.detailsSaveEdit(values);
          }}
          onDeleteRequested={modals.requestDeleteCurrent}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {deleteTarget ? (
        <BuildingDeleteDialog
          open
          building={deleteTarget}
          parentSiteName={
            state.kind === "manage" || state.kind === "manageEditingDetails" || state.kind === "detailsEdit"
              ? state.siteName
              : undefined
          }
          onConfirm={modals.deleteConfirm}
          onDismiss={modals.dismissDeleteConfirm}
          deleting={modals.deleting}
        />
      ) : null}
    </>
  );
};

export default BuildingModals;
