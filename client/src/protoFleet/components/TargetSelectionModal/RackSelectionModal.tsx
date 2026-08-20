import { useCallback, useEffect, useMemo, useState } from "react";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";
import ModalSelectAllFooter from "@/shared/components/Modal/ModalSelectAllFooter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export interface RackSelectionModalProps {
  open: boolean;
  selectedRackIds: string[];
  // Soft default from the topbar SitePicker. A single selected site limits the
  // racks offered to that site; "all sites" passes an empty filter and shows
  // every rack (no regression). Defaulted so callers that don't scope keep the
  // org-wide behavior.
  scope?: SiteFilterFields;
  buildingIds?: bigint[];
  preserveMissingSelections?: boolean;
  onDismiss: () => void;
  onSave: (rackIds: string[]) => void;
}

const RackSelectionModal = ({
  open,
  selectedRackIds,
  scope,
  buildingIds,
  preserveMissingSelections = false,
  onDismiss,
  onSave,
}: RackSelectionModalProps) => {
  const { listRacks } = useDeviceSets();
  const [racks, setRacks] = useState<DeviceSet[]>([]);
  const [draftSelection, setDraftSelection] = useState<Set<string>>(new Set(selectedRackIds));
  const [isLoading, setIsLoading] = useState(true);
  const [hasLoadError, setHasLoadError] = useState(false);

  const siteIds = scope?.siteIds;
  const includeUnassigned = scope?.includeUnassigned;
  const siteIdsKey = (siteIds ?? []).map(String).join(",");
  const buildingIdsKey = (buildingIds ?? []).map(String).join(",");
  // When a site filter is active the list only contains that site's racks, so
  // we can't tell a deleted rack from one that simply belongs to another site.
  // Pruning to the response would silently drop a cross-site schedule's
  // off-site rack targets on save, so preserve preselected ids while scoped and
  // only prune deleted ones under the unscoped (all-sites) list.
  const isScoped =
    (siteIds !== undefined && siteIds.length > 0) ||
    includeUnassigned === true ||
    (buildingIds !== undefined && buildingIds.length > 0);

  useEffect(() => {
    listRacks({
      siteIds,
      includeUnassigned,
      buildingIds,
      onSuccess: (deviceSets) => {
        setRacks(deviceSets);

        if (isScoped || preserveMissingSelections) return;
        const validRackIds = new Set(deviceSets.map((rack) => rack.id.toString()));
        setDraftSelection((current) => new Set([...current].filter((rackId) => validRackIds.has(rackId))));
      },
      onError: (message: string) => {
        setHasLoadError(true);
        pushToast({
          message: message || "Failed to load racks",
          status: STATUSES.error,
        });
      },
      onFinally: () => setIsLoading(false),
    });
    // The ID arrays are recreated by callers; serialized keys keep this effect
    // tied to value changes rather than reference changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listRacks, includeUnassigned, preserveMissingSelections, siteIdsKey, buildingIdsKey]);

  const selectedRackCount = useMemo(
    () => racks.filter((rack) => draftSelection.has(rack.id.toString())).length,
    [draftSelection, racks],
  );
  const missingSelectedRackIds = useMemo(() => {
    if (!preserveMissingSelections) return [];
    const availableIds = new Set(racks.map((rack) => rack.id.toString()));
    return [...draftSelection].filter((rackId) => !availableIds.has(rackId));
  }, [draftSelection, preserveMissingSelections, racks]);

  const allSelected = useMemo(
    () => racks.length > 0 && selectedRackCount === racks.length,
    [selectedRackCount, racks.length],
  );
  const hasRacks = racks.length > 0;
  const showEmptyState = !isLoading && !hasRacks && missingSelectedRackIds.length === 0;

  const toggleRack = useCallback((rackId: string) => {
    setDraftSelection((current) => {
      const next = new Set(current);

      if (next.has(rackId)) {
        next.delete(rackId);
      } else {
        next.add(rackId);
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
      title={hasLoadError ? "Couldn't load racks" : showEmptyState ? "No racks configured" : "Select racks"}
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
        <div className="text-300 text-text-primary-70">Couldn&apos;t load racks. Close this modal and try again.</div>
      ) : showEmptyState ? (
        <div className="text-300 text-text-primary-70">Set up racks to enable more precise targeting.</div>
      ) : (
        <div className="flex flex-col">
          {hasRacks ? (
            <Row divider>
              <label className="flex w-full cursor-pointer items-center gap-4">
                <Checkbox
                  checked={allSelected}
                  partiallyChecked={!allSelected ? selectedRackCount > 0 : false}
                  onChange={() =>
                    setDraftSelection((current) =>
                      allSelected
                        ? new Set<string>()
                        : new Set([...current, ...racks.map((rack) => rack.id.toString())]),
                    )
                  }
                />
                <div className="flex flex-col">
                  <span className="text-emphasis-300 text-text-primary">All racks</span>
                </div>
              </label>
            </Row>
          ) : null}

          {racks.map((rack) => (
            <Row key={rack.id.toString()} divider={false} compact>
              <label className="flex w-full cursor-pointer items-center gap-4">
                <Checkbox
                  checked={draftSelection.has(rack.id.toString())}
                  onChange={() => toggleRack(rack.id.toString())}
                />
                <div className="flex flex-col">
                  <span className="text-emphasis-300 text-text-primary">{rack.label}</span>
                  <span className="text-200 text-text-primary-70">
                    {[rack.placement?.site?.label, rack.placement?.building?.label].filter(Boolean).join(" / ") ||
                      (rack.typeDetails.case === "rackInfo" && rack.typeDetails.value.zone
                        ? rack.typeDetails.value.zone
                        : INACTIVE_PLACEHOLDER)}
                  </span>
                </div>
              </label>
            </Row>
          ))}

          {missingSelectedRackIds.map((rackId) => (
            <Row key={rackId} divider={false} compact>
              <label className="flex w-full cursor-pointer items-center gap-4">
                <Checkbox checked onChange={() => toggleRack(rackId)} />
                <span className="text-emphasis-300 text-text-primary-70">Unavailable rack ({rackId})</span>
              </label>
            </Row>
          ))}

          <ModalSelectAllFooter
            label={`${preserveMissingSelections ? draftSelection.size : selectedRackCount} ${(preserveMissingSelections ? draftSelection.size : selectedRackCount) === 1 ? "rack" : "racks"} selected`}
            onSelectAll={() =>
              setDraftSelection((current) => new Set([...current, ...racks.map((rack) => rack.id.toString())]))
            }
            onSelectNone={() => setDraftSelection(new Set())}
          />
        </div>
      )}
    </Modal>
  );
};

export default RackSelectionModal;
