export interface FirmwareUpdateTarget {
  targetManufacturer: string;
  targetModel: string;
}

// Folds only A-Z, mirroring the server's equalFoldASCII in
// server/internal/infrastructure/files/firmware.go: Unicode look-alikes such as
// U+212A KELVIN SIGN must not match "k", or the client would offer a file the
// server then refuses to install.
export function foldAsciiCase(value: string): string {
  return value.replace(/[A-Z]/g, (letter) => letter.toLowerCase());
}

/**
 * Canonical identity key for a miner (manufacturer, model) target — trimmed and
 * ASCII-case-insensitive, matching the server's firmware compatibility check.
 * Returns null when either part is missing, so absent targets never match
 * anything.
 */
export function minerTargetKey(manufacturer: string | undefined, model: string | undefined): string | null {
  const manufacturerKey = foldAsciiCase(manufacturer?.trim() ?? "");
  const modelKey = foldAsciiCase(model?.trim() ?? "");
  if (!manufacturerKey || !modelKey) return null;
  return `${manufacturerKey}\u0000${modelKey}`;
}
