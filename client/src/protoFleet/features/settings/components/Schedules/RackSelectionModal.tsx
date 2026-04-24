import { useCallback, useEffect, useMemo, useState } from "react";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";
import ModalSelectAllFooter from "@/shared/components/Modal/ModalSelectAllFooter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface RackSelectionModalProps {
  open: boolean;
  selectedRackIds: string[];
  onDismiss: () => void;
  onSave: (rackIds: string[]) => void;
}

const RackSelectionModal = ({ open, selectedRackIds, onDismiss, onSave }: RackSelectionModalProps) => {
  const { listRacks } = useDeviceSets();
  const [racks, setRacks] = useState<DeviceSet[]>([]);
  const [draftSelection, setDraftSelection] = useState<Set<string>>(new Set(selectedRackIds));
  const [isLoading, setIsLoading] = useState(true);
  const [hasLoadError, setHasLoadError] = useState(false);

  useEffect(() => {
    listRacks({
      onSuccess: (deviceSets) => {
        setRacks(deviceSets);

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
  }, [listRacks]);

  const selectedRackCount = useMemo(
    () => racks.filter((rack) => draftSelection.has(rack.id.toString())).length,
    [draftSelection, racks],
  );

  const allSelected = useMemo(
    () => racks.length > 0 && selectedRackCount === racks.length,
    [selectedRackCount, racks.length],
  );
  const hasRacks = racks.length > 0;
  const showEmptyState = !isLoading && !hasRacks;

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
          <Row divider={racks.length > 0}>
            <label className="flex w-full cursor-pointer items-center gap-4">
              <Checkbox
                checked={allSelected}
                partiallyChecked={!allSelected ? selectedRackCount > 0 : false}
                onChange={() =>
                  setDraftSelection(allSelected ? new Set<string>() : new Set(racks.map((rack) => rack.id.toString())))
                }
              />
              <div className="flex flex-col">
                <span className="text-emphasis-300 text-text-primary">All racks</span>
              </div>
            </label>
          </Row>

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
                    {rack.typeDetails.case === "rackInfo" && rack.typeDetails.value.zone
                      ? rack.typeDetails.value.zone
                      : INACTIVE_PLACEHOLDER}
                  </span>
                </div>
              </label>
            </Row>
          ))}

          <ModalSelectAllFooter
            label={`${selectedRackCount} ${selectedRackCount === 1 ? "rack" : "racks"} selected`}
            onSelectAll={() => setDraftSelection(new Set(racks.map((rack) => rack.id.toString())))}
            onSelectNone={() => setDraftSelection(new Set())}
          />
        </div>
      )}
    </Modal>
  );
};

export default RackSelectionModal;
