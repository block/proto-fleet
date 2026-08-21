import ManageSiteModal from "../ManageSiteModal";
import SiteDeleteDialog from "../SiteDeleteDialog";
import SiteSettingsModal from "../SiteSettingsModal";
import SiteBuildingsPicker from "./SiteBuildingsPicker";
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
  const showManage = state.kind === "manageEdit" || state.kind === "manageEditEditingDetails";

  // The manage modal is always backed by a persisted site (Continue creates it
  // up front), so Delete always opens the cascade dialog from the page-level
  // cache rather than discarding a pending create.
  const handleDelete = () => modals.requestDeleteCurrent(sites);

  const manageSite = showManage ? state.site : undefined;

  return (
    <>
      {/* Render ManageSiteModal first so SiteSettingsModal's portal lands
          later in the DOM and naturally stacks on top at the same z-50. */}
      {showManage && manageSite ? (
        <ManageSiteModal
          // Key on the site id so switching directly between sites remounts the
          // modal with a fresh building working set + load-time snapshot,
          // instead of briefly rendering the prior site's entries until the new
          // fetch resolves. Mirrors how the host keys ManageBuildingModal on
          // building.id.
          key={manageSite.id.toString()}
          open
          draft={state.draft}
          site={manageSite}
          onAssignBuildings={modals.manageAssignBuildings}
          onRemoveBuilding={modals.manageRemoveBuilding}
          onCreateBuilding={modals.manageCreateBuilding}
          onCreateBuildings={modals.manageCreateBuildings}
          onEditDetails={modals.manageEditDetails}
          onDeleteRequested={handleDelete}
          onDismiss={modals.dismiss}
          saving={modals.saving}
          buildingsRefreshKey={buildingsRefreshKey}
          unassignedRackCount={modals.manageUnassignedRackCount}
          unassignedMinerCount={modals.manageUnassignedMinerCount}
        />
      ) : null}
      {state.kind === "buildingsPicker" ? (
        <SiteBuildingsPicker
          site={state.site}
          currentBuildings={state.currentBuildings}
          onAssignBuildings={modals.manageAssignBuildings}
          // picker* rather than manage*: with no ManageSiteModal working set to
          // inject into, the created rows only appear if the host's list is
          // refetched.
          onCreateBuilding={modals.pickerCreateBuilding}
          onCreateBuildings={modals.pickerCreateBuildings}
          onDismiss={modals.dismiss}
          saving={modals.saving}
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
