import type { ReactElement } from "react";

// Driver form values are string-keyed input state; each driver form
// module owns its own field names and their encoding into the opaque
// driver_config JSON blob.
export type DriverFormValues = Record<string, string>;

export interface DriverFormFieldsProps {
  // Unique input-id prefix per usage site (e.g. "manual" in the add
  // modal, "device" in the detail modal) so both can mount at once.
  idPrefix: string;
  values: DriverFormValues;
  onChange: (field: string, value: string) => void;
  disabled?: boolean;
}

// A per-driver-type connection form. New protocols register a module
// (plus a select option via the registry) without touching the add or
// detail modals — mirroring the server-side driver adapter registry.
export interface DriverFormModule {
  driverType: string;
  label: string;
  emptyValues: () => DriverFormValues;
  // Decode a driver_config blob into form values. Returns null when the
  // blob is empty or unparseable — e.g. redacted for site:read callers.
  decode: (driverConfig: string) => DriverFormValues | null;
  isValid: (values: DriverFormValues) => boolean;
  // Encode validated form values into the driver_config JSON blob.
  encode: (values: DriverFormValues) => string;
  // One-line connection summary for the list column and detail rows.
  // Returns null when the blob is empty or unparseable.
  summarize: (driverConfig: string) => string | null;
  FormFields: (props: DriverFormFieldsProps) => ReactElement;
}
