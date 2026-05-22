import ManageSiteModal from "../ManageSiteModal";
import SiteDeleteDialog from "../SiteDeleteDialog";
import SiteDetailsModal from "../SiteDetailsModal";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";

interface SiteModalsProps {
  modals: ReturnType<typeof useSiteModals>;
  // SiteWithCounts cache from the host page. Used to resolve the cascade
  // dialog target when Delete is clicked inside SiteDetailsModal (edit mode).
  sites: SiteWithCounts[] | undefined;
  // Pass-through for the ManageSiteModal buildings table. Provided by the
  // host page so the building modal stack shares its useBuildingModals
  // instance with the page-level buildings table.
  onAddBuilding?: (siteId: bigint, siteName?: string) => void;
  onEditBuilding?: (row: BuildingWithCounts, siteName?: string) => void;
  buildingsRefreshKey?: number;
}

const SiteModals = ({ modals, sites, onAddBuilding, onEditBuilding, buildingsRefreshKey }: SiteModalsProps) => {
  const { state, deleteTarget } = modals;
  const showManage =
    state.kind === "manageCreate" ||
    state.kind === "manageEdit" ||
    state.kind === "manageCreateEditingDetails" ||
    state.kind === "manageEditEditingDetails";

  // Delete in create-flow stacked state discards the pending create; edit-flow
  // routes through requestDeleteCurrent which opens the cascade dialog from
  // the page-level cache.
  const handleDelete = () => {
    if (state.kind === "manageCreateEditingDetails") {
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
          onAddBuilding={onAddBuilding && manageSite ? () => onAddBuilding(manageSite.id, manageSite.name) : undefined}
          onEditBuilding={onEditBuilding && manageSite ? (row) => onEditBuilding(row, manageSite.name) : undefined}
          buildingsRefreshKey={buildingsRefreshKey}
        />
      ) : null}
      {state.kind === "detailsCreate" ? (
        <SiteDetailsModal
          open
          mode="create"
          initialValues={state.draft}
          onContinue={modals.detailsContinueCreate}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {state.kind === "manageCreateEditingDetails" ? (
        <SiteDetailsModal
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
        <SiteDetailsModal
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
