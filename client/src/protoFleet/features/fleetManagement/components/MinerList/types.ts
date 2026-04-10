import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { BatchOperation } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";

// DeviceListItem represents a device in the miner list with all data needed for rendering
export type DeviceListItem = {
  deviceIdentifier: string;
  miner: MinerStateSnapshot;
  errors: ErrorMessage[];
  activeBatches: BatchOperation[];
};
