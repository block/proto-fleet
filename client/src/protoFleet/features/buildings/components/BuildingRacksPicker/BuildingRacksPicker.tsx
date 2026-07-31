// The racks picker plus its create hand-off. Hosts render this rather than
// ManageRacksModal directly so the two create entry points ("Create rack" /
// "Create multiple racks") behave the same everywhere the picker is reachable —
// from inside ManageBuildingModal, and standalone from the building page's empty
// state.
//
// Exists as its own component rather than inline in each host because the
// hand-off needs local state (which side of the create modal's toggle to open
// on) and both create paths need the same "already in this building" handling.

import { useMemo, useState } from "react";

import { assignedRackScope, buildingRackScope } from "../ManageBuildingModal/buildingRackScope";
import RackReparentWarningDialog from "../ManageBuildingModal/RackReparentWarningDialog";
import ManageRacksModal from "../ManageRacksModal";
import { type RackSelectionDelta } from "../ManageRacksModal/rackSelectionDelta";
import { type NewRackInput } from "@/protoFleet/api/useDeviceSets";
import RackSettingsModal, {
  type BulkRackPlacement,
} from "@/protoFleet/features/fleetManagement/components/RackSettingsModal";
import { useCreateRack } from "@/protoFleet/features/fleetManagement/hooks/useCreateRack";
import { useCreateRacks } from "@/protoFleet/features/fleetManagement/hooks/useCreateRacks";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// What a host needs to fold a freshly created rack into its own list.
export interface CreatedRack {
  rackId: bigint;
  label: string;
}

interface BuildingRacksPickerProps {
  siteId: bigint;
  buildingId: bigint;
  buildingName: string;
  currentRackIds: bigint[];
  // Save. Called only once any reparent in the delta has been confirmed — the
  // warning lives here, next to the choice that triggers it, so every host gets
  // it without wiring the dialog itself.
  onConfirm: (delta: RackSelectionDelta) => void;
  // Every created rack is placed in this building by the create call itself, so
  // there is no membership left to commit — the host folds these into its list
  // and the flow closes.
  onCreated: (racks: CreatedRack[]) => void;
  onDismiss: () => void;
  saving?: boolean;
}

const BuildingRacksPicker = ({
  siteId,
  buildingId,
  buildingName,
  currentRackIds,
  onConfirm,
  onCreated,
  onDismiss,
  saving = false,
}: BuildingRacksPickerProps) => {
  // null = showing the picker; otherwise which side of the create modal's
  // Single / Multiple toggle to open on.
  const [creating, setCreating] = useState<"single" | "multiple" | null>(null);

  // Fetch scopes are derived here rather than taken as props so every host
  // agrees: they come from the building's OWN site, not the header SitePicker,
  // which on /buildings/:id may be an unrelated persisted selection. Only the
  // broadened ("Show assigned racks") scope consults the header, to decide
  // whether a cross-site reparent is on the table.
  const allSites = useFleetStore((state) => state.ui.activeSite.kind === "all");
  const scope = useMemo(() => buildingRackScope(siteId), [siteId]);
  const assignedScope = useMemo(() => assignedRackScope(siteId, allSites), [siteId, allSites]);

  // Shared with RacksPage's "Add rack" so the create semantics — toast,
  // double-click guard — can't drift. Its site-strip conflict path is
  // unreachable here: that only fires for seeded miners, and this create seeds
  // none.
  const { createRack, creating: creatingOne } = useCreateRack({
    onCreated: (rackId, formData) => {
      onCreated([{ rackId, label: formData.label }]);
      onDismiss();
    },
  });
  const { createRacks, creating: creatingMany } = useCreateRacks();

  // Pending reparent confirm — set when the Save carries racks that are placed
  // somewhere else, since assigning them here moves the rack and every miner in
  // it. Cancelling leaves the picker open with the selection intact.
  const [reparenting, setReparenting] = useState<RackSelectionDelta | null>(null);

  const handleConfirm = (delta: RackSelectionDelta) => {
    if (delta.reassigned.length > 0) {
      setReparenting(delta);
      return;
    }
    onConfirm(delta);
  };

  const handleCreateBulk = async (racks: NewRackInput[], placement: BulkRackPlacement) => {
    const { created, errors } = await createRacks(racks, placement);
    if (created.length > 0) {
      onCreated(created.map((rack) => ({ rackId: rack.id, label: rack.label })));
      onDismiss();
    }
    // Non-empty leaves the create modal open with the rejected rows marked.
    return errors;
  };

  if (creating) {
    return (
      <RackSettingsModal
        show
        // Zone defaulting reads this to prefill from the most recent rack; the
        // picker fetches its own paginated list, so there is nothing worth
        // plumbing through for a convenience default.
        existingRacks={[]}
        // Both placement fields are locked: the operator opened create from
        // inside this building, and a rack created elsewhere would vanish from
        // the list they were looking at.
        defaultSiteId={siteId}
        defaultBuilding={{ id: buildingId, label: buildingName }}
        onSubmit={createRack}
        // Present unconditionally — this is what shows the Single / Multiple
        // toggle inside the create modal.
        onSubmitBulk={handleCreateBulk}
        initialCreateVariant={creating}
        onDismiss={() => setCreating(null)}
        saving={saving || creatingOne || creatingMany}
      />
    );
  }

  return (
    <>
      <ManageRacksModal
        open
        siteId={siteId}
        currentBuildingId={buildingId}
        scope={scope}
        assignedScope={assignedScope}
        allSites={allSites}
        buildingName={buildingName}
        initialSelectedRackIds={currentRackIds}
        onDismiss={onDismiss}
        onConfirm={handleConfirm}
        saving={saving}
        // Swapping to create abandons the pending selection: the picker's Save is
        // what writes membership, so nothing was staged on the server.
        onCreateNewLaunch={() => setCreating("single")}
        onCreateMultipleLaunch={() => setCreating("multiple")}
      />
      {reparenting ? (
        <RackReparentWarningDialog
          racks={reparenting.reassigned}
          buildingName={buildingName}
          onCancel={() => setReparenting(null)}
          // Clearing first: accepting the warning is what authorizes the write,
          // and the host may keep the picker open on failure to retry.
          onConfirm={() => {
            setReparenting(null);
            onConfirm(reparenting);
          }}
        />
      ) : null}
    </>
  );
};

export default BuildingRacksPicker;
