import { useCallback, useEffect, useMemo, useState } from "react";

import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import { type ColConfig, type ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type RackColumn = "label" | "zone" | "miners";

const COL_TITLES: ColTitles<RackColumn> = {
  label: "Name",
  zone: "Zone",
  miners: "Miners",
};

const ACTIVE_COLS: RackColumn[] = ["label", "zone", "miners"];

const INACTIVE_PLACEHOLDER = "—";

type RackListItem = {
  id: string;
  label: string;
  zone: string;
  deviceCount: number;
  buildingId: bigint | undefined;
};

const toListItem = (deviceSet: DeviceSet): RackListItem => {
  const rackInfo = deviceSet.typeDetails.case === "rackInfo" ? deviceSet.typeDetails.value : undefined;
  return {
    id: deviceSet.id.toString(),
    label: deviceSet.label,
    zone: rackInfo?.zone ?? "",
    deviceCount: deviceSet.deviceCount,
    buildingId: rackInfo?.buildingId,
  };
};

interface RackPickerModalProps {
  show: boolean;
  // Modal title — callers specialize (e.g. "Add racks to Alpha") so the
  // picker reads naturally inside the calling flow.
  title?: string;
  confirmButtonText?: string;
  // Optional pre-filter applied client-side after fetching the rack
  // list. Use to hide racks that are already members of the target
  // (e.g. excludeBuildingId = current building so "Add racks" only
  // surfaces racks not already there).
  excludeBuildingId?: bigint;
  // Optional disable predicate for additional UI-side gating (e.g.
  // "rack already in another building" → render disabled with reason
  // surfaced through the row label). The picker leaves these rows in
  // the list but greyed.
  isRowDisabled?: (item: RackListItem) => boolean;
  onDismiss: () => void;
  onConfirm: (selectedRackIds: bigint[]) => void;
}

// Multi-select rack picker. Sibling to MinerPickerModal — the /fleet
// row "Add racks" flow opens this against a building / site target,
// the caller dispatches the rack-reassignment RPC on confirm.
const RackPickerModal = ({
  show,
  title = "Select racks",
  confirmButtonText = "Continue",
  excludeBuildingId,
  isRowDisabled,
  onDismiss,
  onConfirm,
}: RackPickerModalProps) => {
  const { listRacks } = useDeviceSets();
  const [racks, setRacks] = useState<DeviceSet[] | undefined>(undefined);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedIds, setSelectedIds] = useState<string[]>([]);

  // Trigger the fetch + state reset when the modal opens. Defers to a
  // microtask so the lint rule against synchronous setState-in-effect
  // doesn't flag the reset (matches the queueMicrotask pattern used by
  // MinerSelectionList for the same reason).
  useEffect(() => {
    if (!show) return;
    queueMicrotask(() => {
      setSelectedIds([]);
      setRacks(undefined);
      setLoadError(null);
    });
    void listRacks({
      onSuccess: (deviceSets) => setRacks(deviceSets),
      onError: (message) => setLoadError(message),
    });
  }, [show, listRacks]);

  const items: RackListItem[] = useMemo(() => {
    if (!racks) return [];
    // Filter out racks already assigned to the target building so the
    // picker only surfaces candidates the operator can actually add.
    const filtered =
      excludeBuildingId !== undefined
        ? racks.filter((deviceSet) => {
            const rackInfo = deviceSet.typeDetails.case === "rackInfo" ? deviceSet.typeDetails.value : undefined;
            return rackInfo?.buildingId !== excludeBuildingId;
          })
        : racks;
    return filtered.map(toListItem).sort((a, b) => a.label.localeCompare(b.label));
  }, [racks, excludeBuildingId]);

  const colConfig = useMemo<ColConfig<RackListItem, string, RackColumn>>(
    () => ({
      label: {
        component: (item) => <span className="truncate text-emphasis-300">{item.label || INACTIVE_PLACEHOLDER}</span>,
        width: "min-w-44",
      },
      zone: {
        component: (item) => <span>{item.zone || INACTIVE_PLACEHOLDER}</span>,
        width: "min-w-28",
      },
      miners: {
        component: (item) => <span>{item.deviceCount.toString()}</span>,
        width: "min-w-20",
      },
    }),
    [],
  );

  const handleSelectionChange = useCallback((next: string[]) => setSelectedIds(next), []);

  const handleConfirm = useCallback(() => {
    if (selectedIds.length === 0) return;
    onConfirm(selectedIds.map((id) => BigInt(id)));
  }, [onConfirm, selectedIds]);

  if (!show) return null;

  return (
    <Modal
      open={show}
      title={title}
      size="large"
      className="flex !h-[calc(100vh-(--spacing(32)))] max-h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      onDismiss={onDismiss}
      divider={false}
      buttons={[
        {
          text: confirmButtonText,
          variant: "primary",
          onClick: handleConfirm,
          disabled: selectedIds.length === 0,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col">
        {loadError ? (
          <Callout className="mb-4 shrink-0" intent="danger" prefixIcon={<Alert />} title={loadError} />
        ) : null}
        {racks === undefined && !loadError ? (
          <div className="flex flex-1 items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        ) : (
          <List<RackListItem, string, RackColumn>
            activeCols={ACTIVE_COLS}
            colTitles={COL_TITLES}
            colConfig={colConfig}
            items={items}
            itemKey="id"
            itemSelectable
            customSelectedItems={selectedIds}
            customSetSelectedItems={handleSelectionChange}
            customSelectionMode={selectedIds.length === 0 ? "none" : "subset"}
            onSelectionModeChange={() => undefined}
            isRowSelectable={isRowDisabled ? (item) => !isRowDisabled(item) : undefined}
            hideTotal
          />
        )}
      </div>
    </Modal>
  );
};

export default RackPickerModal;
