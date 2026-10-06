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

// Match Go strings.TrimSpace and release_channel_pair_key in migration 148.
// JavaScript trim() drops BOM (U+FEFF) and preserves NEL (U+0085), unlike Go.
export function trimMinerTarget(value: string): string {
  return value.replace(
    /^[\t\n\v\f\r\u0020\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+|[\t\n\v\f\r\u0020\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+$/g,
    "",
  );
}

/**
 * Canonical identity key for a miner (manufacturer, model) target — trimmed and
 * ASCII-case-insensitive, matching the server's firmware compatibility check.
 * Returns null when either part is missing, so absent targets never match
 * anything.
 */
export function minerTargetKey(manufacturer: string | undefined, model: string | undefined): string | null {
  const manufacturerKey = foldAsciiCase(trimMinerTarget(manufacturer ?? ""));
  const modelKey = foldAsciiCase(trimMinerTarget(model ?? ""));
  if (!manufacturerKey || !modelKey) return null;
  return JSON.stringify([manufacturerKey, modelKey]);
}
