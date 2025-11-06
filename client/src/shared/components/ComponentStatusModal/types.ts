import type { ReactNode } from "react";

// Component types
export type ComponentType = "hashboard" | "psu" | "fan" | "controlBoard";

export interface ComponentError {
  id: string;
  componentType: ComponentType;
  componentName: string;
  title: string;
  message: string;
  timestamp?: number;
  details?: string;
  notificationError: any; // API-specific error type
  severity?: "warning" | "error" | "critical"; // Error severity level
}

// Component details for displaying metrics, visualization, and metadata
export interface ComponentMetric {
  label: string;
  value: ReactNode; // Can be a value component like TemperatureValue, or just a string
}

export interface ComponentMetadata {
  serialNumber?: string;
  model?: string;
  installedOn?: string; // Date string MM/DD/YY format
  age?: string; // Auto-formatted age string
  [key: string]: string | undefined; // Allow additional fields
}

// Props for the ComponentStatusModal
export interface ComponentStatusModalProps {
  summary: string; // e.g., "Hashboard 3 has multiple issues"
  componentType: ComponentType;
  issues: ComponentError[];
  metrics?: ComponentMetric[];
  metadata?: ComponentMetadata;
  navigateBack?: () => void;
  onDismiss: () => void;
}
