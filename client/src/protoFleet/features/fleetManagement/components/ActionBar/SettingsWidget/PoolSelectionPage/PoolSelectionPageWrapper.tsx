import { create } from "@bufbuild/protobuf";

import {
  type MinerListFilter,
  MinerListFilterSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { MinerSelection } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import type { DeviceFilterCriteria } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import type { SelectionMode } from "@/shared/components/List";

interface PoolSelectionPageWrapperProps {
  open?: boolean;
  selectionMode: SelectionMode;
  poolNeededCount?: number;
  filterCriteria?: DeviceFilterCriteria;
  minerListFilter?: MinerListFilter;
  selectedMiners?: MinerSelection[];
  onApplied?: (operationId: string) => void;
  onDismiss: () => void;
}

const PoolSelectionPageWrapper = ({
  open = true,
  selectionMode,
  poolNeededCount,
  filterCriteria,
  minerListFilter,
  selectedMiners,
  onApplied,
  onDismiss,
}: PoolSelectionPageWrapperProps) => {
  if (!open || selectionMode === "none") return null;
  const filter =
    selectionMode === "all"
      ? (minerListFilter ??
        create(MinerListFilterSchema, {
          deviceStatus: filterCriteria?.deviceStatus ? [filterCriteria.deviceStatus] : [],
          pairingStatuses: filterCriteria?.pairingStatus ? [filterCriteria.pairingStatus] : [],
        }))
      : undefined;
  return (
    <ConfigurationSelector
      deviceIdentifiers={selectedMiners?.map((miner) => miner.deviceIdentifier) ?? []}
      filter={filter}
      selectedCount={selectionMode === "all" ? poolNeededCount : selectedMiners?.length}
      onApplied={onApplied}
      onDismiss={onDismiss}
    />
  );
};

export default PoolSelectionPageWrapper;
