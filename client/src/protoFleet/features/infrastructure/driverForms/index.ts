import { modbusTcpFormModule } from "./modbusTcp";
import type { DriverFormModule } from "./types";

const modules: DriverFormModule[] = [modbusTcpFormModule];

const modulesByDriverType = new Map(modules.map((formModule) => [formModule.driverType, formModule]));

export const DEFAULT_DRIVER_TYPE = modbusTcpFormModule.driverType;

export const driverTypeOptions = modules.map((formModule) => ({
  value: formModule.driverType,
  label: formModule.label,
}));

export const getDriverFormModule = (driverType: string): DriverFormModule | undefined =>
  modulesByDriverType.get(driverType);

export const getDriverTypeLabel = (driverType: string): string =>
  modulesByDriverType.get(driverType)?.label ?? driverType;

// Connection summary for the list column and detail rows; null when the
// driver type has no registered module or driver_config is empty or
// unparseable (e.g. redacted for site:read callers).
export const summarizeDriverConfig = (driverType: string, driverConfig: string): string | null =>
  modulesByDriverType.get(driverType)?.summarize(driverConfig) ?? null;

export type { DriverFormModule, DriverFormValues } from "./types";
