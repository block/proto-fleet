// The firmware component of a CT1 PSU capsule, as the firmware store and the
// rollout contract name it. A file, assignment or rollout that names no
// component is for its target's main firmware.
export const PSU_FIRMWARE_COMPONENT = "psu";

// A firmware component as the UI names it after its model, as in
// "Proto CT1 PSU". The main firmware has no name of its own.
export const firmwareComponentLabel = (component?: string): string =>
  component === PSU_FIRMWARE_COMPONENT ? "PSU" : (component ?? "");

// The kinds of firmware a Proto Container takes, as the Files tab headings and
// the release channel rows name them. The main firmware of a container is its
// hashboard firmware.
export const HASHBOARD_FIRMWARE_LABEL = "Hashboard firmware";
export const PSU_FIRMWARE_LABEL = "PSU firmware";
export const containerFirmwareLabel = (component?: string): string =>
  component === PSU_FIRMWARE_COMPONENT ? PSU_FIRMWARE_LABEL : HASHBOARD_FIRMWARE_LABEL;
