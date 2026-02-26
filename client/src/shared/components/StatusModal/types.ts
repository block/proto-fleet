import type { ReactNode } from "react";
import type { ButtonProps } from "@/shared/components/ButtonGroup";

// Component address type for navigation (used in stories/tests)
// Implementers should provide their own address type via generics
export interface ComponentAddress {
  source: string;
  componentIndex?: number;
  [key: string]: any; // Allow additional fields for flexibility
}

// Generic error data type for Status modal
// Both protoOS and protoFleet transform their errors to this type
// The modal content components decide how to display these fields
export type ErrorData = {
  componentName: string; // Component name (e.g., "Hashboard 1", "Fan 3")
  message: string; // Error message/description
  timestamp?: number; // Unix timestamp in seconds (will be formatted if present)
  onClick?: () => void; // Optional click handler for navigation
};

export type MinerStatusModalProps = {
  title: string;
  subtitle?: string;
  errors: {
    hashboard: ErrorData[];
    psu: ErrorData[];
    fan: ErrorData[];
    controlBoard: ErrorData[];
    other: ErrorData[];
  };
  isSleeping?: boolean;
  isMining?: boolean;
  isOffline?: boolean;
  needsAuthentication?: boolean;
  needsMiningPool?: boolean;
};

/**
 * Types for ComponentStatusModalContent
 */
export type ComponentType = "hashboard" | "psu" | "fan" | "controlBoard" | "other";

// Component details for displaying metrics, visualization, and metadata
export interface ComponentMetric {
  label: string;
  value: ReactNode; // Can be a value component like TemperatureValue, or just a string
}

export interface ComponentMetadata {
  [key: string]: {
    value?: string | number;
    label: string;
  };
}

// Props for the ComponentStatusModal
export interface ComponentStatusModalProps {
  summary?: string; // e.g., "Hashboard 3 has multiple errors"
  componentType: ComponentType;
  errors: ErrorData[];
  metrics?: ComponentMetric[];
  metadata?: ComponentMetadata;
}

/**
 * Types for StatusModal container
 */

/**
 * Complete miner status data including modal config
 */
export interface MinerStatusData {
  props: MinerStatusModalProps;
  title: string;
  buttons: ButtonProps[];
  onDismiss: () => void;
}

/**
 * Complete component status data including modal config
 */
export interface ComponentStatusData {
  props: ComponentStatusModalProps;
  title: string;
  buttons: ButtonProps[];
  onDismiss: () => void;
  onNavigateBack?: () => void; // Optional back navigation handler
}

/**
 * Props for the prop-driven StatusModal container
 */
export interface StatusModalProps<TComponentAddress = any> {
  /** If undefined, shows miner status. If defined, shows component status */
  componentAddress?: TComponentAddress;
  /** Function to get miner status data and config */
  getMinerStatus: () => MinerStatusData;
  /** Function to get component status data and config. Returns undefined if component not found */
  getComponentStatus: (address: TComponentAddress) => ComponentStatusData | undefined;
  /** Whether the modal is open */
  open?: boolean;
  /** Whether to show back navigation (only applies when component is defined) */
  showBackButton?: boolean;
}
