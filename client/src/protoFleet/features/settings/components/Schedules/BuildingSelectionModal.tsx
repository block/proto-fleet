import { useCallback, useEffect, useMemo, useState } from "react";

import { useBuildings } from "@/protoFleet/api/buildings";
import type { BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";
import ModalSelectAllFooter from "@/shared/components/Modal/ModalSelectAllFooter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface BuildingSelectionModalProps {
  open: boolean;
  selectedBuildingIds: string[];
  // Soft default from the topbar SitePicker. A single selected site limits the
  // buildings offered to that site; "all sites" passes an empty filter and
  // lists every building (including site-unassigned ones). Mirrors
  // RackSelectionModal.
  scope?: SiteFilterFields;
  onDismiss: () => void;
  onSave: (buildingIds: string[]) => void;
}

const BuildingSelectionModal = ({
  open,
  selectedBuildingIds,
  scope,
  onDismiss,
  onSave,
}: BuildingSelectionModalProps) => {
  const { listBuildings } = useBuildings();
  const [buildings, setBuildings] = useState<BuildingWithCounts[]>([]);
  const [draftSelection, setDraftSelection] = useState<Set<string>>(new Set(selectedBuildingIds));
  const [isLoading, setIsLoading] = useState(true);
  const [hasLoadError, setHasLoadError] = useState(false);

  const siteIds = scope?.siteIds;
  const includeUnassigned = scope?.includeUnassigned;
  // While scoped the list holds only the active site's buildings, so we can't
  // distinguish a deleted building from an off-site one. Preserve preselected
  // ids when scoped (don't drop a cross-site schedule's off-site building
  // targets); only prune deleted ones under the unscoped list.
  const isScoped = (siteIds !== undefined && siteIds.length > 0) || includeUnassigned === true;

  useEffect(() => {
    void listBuildings({
      siteIds,
      includeUnassigned,
      onSuccess: (rows) => {
        setBuildings(rows);
        if (isScoped) return;
        const validIds = new Set(rows.map((row) => (row.building?.id ?? 0n).toString()));
        setDraftSelection((current) => new Set([...current].filter((buildingId) => validIds.has(buildingId))));
      },
      onError: (message: string) => {
        setHasLoadError(true);
        pushToast({ message: message || "Failed to load buildings", status: STATUSES.error });
      },
      onFinally: () => setIsLoading(false),
    });
    // siteIds is a bigint[]; serialize for a stable dep so re-runs only fire
    // when the active-site selection actually changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listBuildings, includeUnassigned, (siteIds ?? []).map(String).join(",")]);

  const selectableBuildingIds = useMemo(
    () => buildings.map((building) => (building.building?.id ?? 0n).toString()).filter((id) => id !== "0"),
    [buildings],
  );
  const selectedBuildingCount = useMemo(
    () => selectableBuildingIds.filter((id) => draftSelection.has(id)).length,
    [draftSelection, selectableBuildingIds],
  );
  const allSelected = selectableBuildingIds.length > 0 && selectedBuildingCount === selectableBuildingIds.length;
  const showEmptyState = !isLoading && selectableBuildingIds.length === 0;

  const toggleBuilding = useCallback((buildingId: string) => {
    setDraftSelection((current) => {
      const next = new Set(current);
      if (next.has(buildingId)) {
        next.delete(buildingId);
      } else {
        next.add(buildingId);
      }
      return next;
    });
  }, []);

  if (!open) {
    return null;
  }

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={hasLoadError ? "Couldn't load buildings" : showEmptyState ? "No buildings configured" : "Select buildings"}
      divider={false}
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: () => onSave(Array.from(draftSelection)),
          dismissModalOnClick: false,
          disabled: hasLoadError,
        },
      ]}
    >
      {isLoading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : hasLoadError ? (
        <div className="text-300 text-text-primary-70">
          Couldn&apos;t load buildings. Close this modal and try again.
        </div>
      ) : showEmptyState ? (
        <div className="text-300 text-text-primary-70">Set up buildings to enable building-wide targeting.</div>
      ) : (
        <div className="flex flex-col">
          <Row divider>
            <label className="flex w-full cursor-pointer items-center gap-4">
              <Checkbox
                checked={allSelected}
                partiallyChecked={!allSelected ? selectedBuildingCount > 0 : false}
                onChange={() => setDraftSelection(allSelected ? new Set<string>() : new Set(selectableBuildingIds))}
              />
              <div className="flex flex-col">
                <span className="text-emphasis-300 text-text-primary">All buildings</span>
              </div>
            </label>
          </Row>

          {buildings.map((building) => {
            const buildingId = (building.building?.id ?? 0n).toString();
            return (
              <Row key={buildingId} divider={false} compact>
                <label className="flex w-full cursor-pointer items-center gap-4">
                  <Checkbox checked={draftSelection.has(buildingId)} onChange={() => toggleBuilding(buildingId)} />
                  <div className="flex flex-col">
                    <span className="text-emphasis-300 text-text-primary">{building.building?.name}</span>
                  </div>
                </label>
              </Row>
            );
          })}

          <ModalSelectAllFooter
            label={`${selectedBuildingCount} ${selectedBuildingCount === 1 ? "building" : "buildings"} selected`}
            onSelectAll={() => setDraftSelection(new Set(selectableBuildingIds))}
            onSelectNone={() => setDraftSelection(new Set())}
          />
        </div>
      )}
    </Modal>
  );
};

export default BuildingSelectionModal;
