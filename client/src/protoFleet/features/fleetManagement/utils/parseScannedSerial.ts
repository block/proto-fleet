/**
 * Parse the serial number out of the text decoded from a scanned miner QR
 * code (or barcode).
 *
 * Miner serial labels observed in the field encode the value with a short
 * prefix, e.g. `SN:1234567890123456`. Some manufacturers use `S/N`, a space,
 * or an `=` separator, and handheld scanners occasionally append a trailing
 * newline. We normalize all of these to the bare serial the server matches
 * verbatim against `device.serial_number`.
 *
 * Matching intentionally stays permissive on the *prefix* but strict on the
 * *value*: we only strip a leading `SN`/`S/N`/`SERIAL` token when it is
 * followed by a separator, so a serial that legitimately begins with the
 * letters "SN" is not mangled.
 */

// Leading label token (SN, S/N, SERIAL, SERIAL NO/NUMBER) followed by a
// *required* separator — either punctuation (`:`, `=`, `#`) or whitespace.
// Requiring the separator is what keeps a serial that legitimately begins
// with the letters "SN" (e.g. "SNX9000") from being truncated: with no
// separator after the token, the prefix simply does not match.
// Case-insensitive.
const SERIAL_PREFIX = /^\s*(?:s\/?n|serial(?:\s*(?:no|number))?)(?:\s*[:=#]\s*|\s+)/i;

/**
 * Extract a bare serial number from raw scanned text. Returns an empty string
 * when nothing usable remains (caller should treat that as "not a serial").
 */
export function parseScannedSerial(raw: string): string {
  if (!raw) return "";

  // Collapse to the first line — scanners may append CR/LF, and multi-line
  // payloads (rare) put the serial first.
  const firstLine = raw.split(/[\r\n]/, 1)[0] ?? "";

  const withoutPrefix = firstLine.replace(SERIAL_PREFIX, "");

  return withoutPrefix.trim();
}
