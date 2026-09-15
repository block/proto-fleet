/**
 * ProtoOS-specific StatusModal types
 */

import type { ComponentAddress } from "@/protoOS/store/utils/componentSelection";
export type { ComponentAddress };

/**
 * Props for the ProtoOS StatusModal wrapper component
 *
 * This wrapper encapsulates all integration logic with the protoOS store
 * and provides a simple API for consumers.
 */
export interface ProtoOSStatusModalProps {
  /** Whether the modal is open */
  open?: boolean;

  /** Callback when modal should be closed */
  onClose: () => void;

  /** Optional initial component to display (defaults to miner view) */
  componentAddress?: ComponentAddress;

  /** Whether to show back button in component views (defaults to true) */
  showBackButton?: boolean;
}
