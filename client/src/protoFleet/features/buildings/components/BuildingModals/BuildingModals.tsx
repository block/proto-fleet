import BuildingDeleteDialog from "../BuildingDeleteDialog";
import BuildingSettingsModal from "../BuildingSettingsModal";
import ManageBuildingModal from "../ManageBuildingModal";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";

interface BuildingModalsProps {
  modals: ReturnType<typeof useBuildingModals>;
  // Site list powers BuildingSettingsModal's Site dropdown in create mode.
  // Hosts already fetch sites for their own rendering, so we forward the
  // existing list instead of fetching again here.
  sites: SiteWithCounts[];
}

// Renders whichever building modal is on top of the stack. The modals hook
// owns BuildingWithCounts in its edit-bearing states, so this host needs no
// external buildings cache to resolve cascade-dialog rack_count.
const BuildingModals = ({ modals, sites }: BuildingModalsProps) => {
  const { state, deleteTarget } = modals;
  const showManage = state.kind === "manage" || state.kind === "manageEditingDetails";

  return (
    <>
      {/* ManageBuildingModal renders first so BuildingSettingsModal's portal
          lands later in the DOM and naturally stacks on top. */}
      {showManage && state.row.building ? (
        // key on building.id remounts ManageBuildingModal when the
        // host swaps to a different building without first closing
        // (e.g. clicking a second row in the buildings table while
        // the modal is open). The remount drops the prior building's
        // entries / initialPlacementRef so Save can't fire against
        // a stale snapshot in the load-pending window.
        <ManageBuildingModal
          key={state.row.building.id.toString()}
          open
          building={state.row.building}
          siteName={state.siteName}
          onDismiss={modals.dismiss}
          onEditDetails={modals.manageEditDetails}
          onDeleteRequested={modals.requestDeleteCurrent}
          // Rack-placement saves don't go through useBuildingModals'
          // create/update/delete callbacks, so refetchBuildings doesn't
          // fire on its own. Surface refreshBuildings here so host
          // caches (rack_count, layout) re-pull from the server.
          onSaved={modals.refreshBuildings}
          unassignedMinerCount={modals.manageUnassignedMinerCount}
        />
      ) : null}
      {state.kind === "detailsCreate" ? (
        <BuildingSettingsModal
          open
          mode="create"
          initialValues={state.draft}
          parentSiteLabel={state.siteName}
          sites={sites}
          initialSiteId={state.siteId}
          onSave={async (values, siteId) => {
            await modals.detailsCreate(values, siteId);
          }}
          onDismiss={modals.dismiss}
          saving={modals.saving}
        />
      ) : null}
      {/* Edit-mode renders the same modal whether it's standalone
          (detailsEdit) or stacked on top of ManageBuildingModal
          (manageEditingDetails). The only difference between the two
          states is whether ManageBuildingModal sits underneath; the
          settings-modal JSX itself is identical. */}
      {state.kind === "detailsEdit" || state.kind === "manageEditingDetails" ? (
        <BuildingSettingsModal
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
