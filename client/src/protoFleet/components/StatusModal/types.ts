/**
 * ProtoFleet-specific StatusModal types
 */

import type { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

/**
 * Component address for navigation to ComponentStatusModal
 * In ProtoFleet, we use the component type from the errors API
 * deviceId is included to ensure uniqueness across devices
 * componentId is the ID from the API (currently index as string, will be unique ID in future)
 */
export interface ComponentAddress {
  deviceId: string;
  componentType: ErrorComponentType;
  componentId: string; // Component ID from API (for RESULT_VIEW_COMPONENT calls)
}

/**
 * Props for the ProtoFleet StatusModal wrapper component
 *
 * This wrapper encapsulates all integration logic with the ProtoFleet store
 * and provides a simple API for consumers.
 */
export interface ProtoFleetStatusModalProps {
  /** Controls modal visibility */
  open?: boolean;

  /** Callback when modal should be closed */
  onClose: () => void;

  /** The device identifier (miner ID) to show status for */
  deviceId: string;

  /** Optional miner data — if not provided, status info will be limited */
  miner?: MinerStateSnapshot;

  /** Optional initial component to display (defaults to miner view) */
  componentAddress?: ComponentAddress;

  /** Whether to show back button in component views (defaults to true) */
  showBackButton?: boolean;
}
