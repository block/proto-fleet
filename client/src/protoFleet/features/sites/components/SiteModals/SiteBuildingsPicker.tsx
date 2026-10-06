// The buildings picker as a standalone flow, with no ManageSiteModal behind
// it. Hosts that already render the site (SiteDetailPage) open membership
// editing directly; stacking the whole manage surface underneath would put a
// layer on screen the operator never asked for.
//
// Exists as its own component rather than inline in SiteModals because the
// "New building" hand-off needs local state, and SiteModals is otherwise a
// pure switch over modal state.

import { useState } from "react";
import { create } from "@bufbuild/protobuf";

import ManageBuildingsModal from "../ManageBuildingsModal";
import {
  type BuildingFormValues,
  type BulkCreateBuildingError,
  emptyBuildingFormValues,
  type NewBuildingInput,
} from "@/protoFleet/api/buildings";
import { type Building } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type Site, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import BuildingSettingsModal from "@/protoFleet/features/buildings/components/BuildingSettingsModal";
import { type BulkBuildingCreateResult, type SiteBuildingRef } from "@/protoFleet/features/sites/hooks/useSiteModals";

interface SiteBuildingsPickerProps {
  site: Site;
  // Buildings already in the site, from the host's own list.
  currentBuildings: SiteBuildingRef[];
  onAssignBuildings: (delta: { added: bigint[]; removed: bigint[] }) => Promise<boolean>;
  onCreateBuilding: (values: BuildingFormValues) => Promise<Building | null>;
  onCreateBuildings: (buildings: NewBuildingInput[]) => Promise<BulkBuildingCreateResult>;
  onDismiss: () => void;
  saving: boolean;
}

const SiteBuildingsPicker = ({
  site,
  currentBuildings,
  onAssignBuildings,
  onCreateBuilding,
  onCreateBuildings,
  onDismiss,
  saving,
}: SiteBuildingsPickerProps) => {
  // null = showing the picker; otherwise which side of the create modal's
  // Single / Multiple toggle to open on.
  const [creating, setCreating] = useState<"single" | "multiple" | null>(null);

  const handleConfirm = async (delta: { added: { buildingId: bigint; label: string }[]; removed: bigint[] }) => {
    const ok = await onAssignBuildings({ added: delta.added.map((a) => a.buildingId), removed: delta.removed });
    // A failed write leaves the picker open so the selection can be retried.
    if (ok) onDismiss();
  };

  // Create already attaches the building to this site, so there is nothing
  // left to commit — close the whole flow rather than returning to a picker
  // whose only new row would already read "In this site". The host's list
  // refreshes because pickerCreateBuilding(s) calls refetchBuildings.
  const handleCreateSave = async (values: BuildingFormValues) => {
    const created = await onCreateBuilding(values);
    if (created) onDismiss();
  };

  const handleCreateBulkSave = async (buildings: NewBuildingInput[]): Promise<BulkCreateBuildingError[]> => {
    const { created, errors } = await onCreateBuildings(buildings);
    if (created.length > 0) onDismiss();
    return errors;
  };

  if (creating) {
    return (
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={emptyBuildingFormValues()}
        // One-element catalog: the Site dropdown is locked to this site
        // anyway, so there is no reason to plumb the host's whole list in.
        sites={[create(SiteWithCountsSchema, { site })]}
        initialSiteId={site.id}
        parentSiteLabel={site.name}
        onSave={async (values) => {
          await handleCreateSave(values);
        }}
        // Present unconditionally — this is what shows the Single / Multiple
        // toggle inside the create modal.
        onSaveBulk={handleCreateBulkSave}
        initialCreateVariant={creating}
        existingBuildingNames={currentBuildings.map((b) => b.name)}
        onDismiss={() => setCreating(null)}
        saving={saving}
      />
    );
  }

  return (
    <ManageBuildingsModal
      open
      siteId={site.id}
      initialSelectedBuildingIds={currentBuildings.map((b) => b.id)}
      onDismiss={onDismiss}
      onConfirm={handleConfirm}
      saving={saving}
      // Swapping to create abandons the pending selection, same as in
      // ManageSiteModal: the picker's Save is what writes membership. Both
      // buttons are offered here — this host always wires the batch RPC.
      onCreateNewLaunch={() => setCreating("single")}
      onCreateMultipleLaunch={() => setCreating("multiple")}
    />
  );
};

export default SiteBuildingsPicker;
