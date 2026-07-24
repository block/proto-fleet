export interface FirmwareUpdateTarget {
  targetManufacturer: string;
  targetModel: string;
}

/**
 * Canonical identity key for a miner (manufacturer, model) target — trimmed and
 * case-insensitive. Returns null when either part is missing, so absent targets
 * never match anything.
 */
export function minerTargetKey(manufacturer: string | undefined, model: string | undefined): string | null {
  const manufacturerKey = manufacturer?.trim().toLowerCase() ?? "";
  const modelKey = model?.trim().toLowerCase() ?? "";
  if (!manufacturerKey || !modelKey) return null;
  return `${manufacturerKey}\u0000${modelKey}`;
}
