import type { TelemetryState } from "../contract";

export const createEmptyTelemetryState = (): TelemetryState => ({
  resourceByScope: new Map(),
  rollupsByEntity: new Map(),
  hashboards: new Map(),
  asics: new Map(),
  psus: new Map(),
  fans: new Map(),
  coolingModeByOwner: new Map(),
  asicLoadByHashboard: new Map(),
});
