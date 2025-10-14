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
}
