import { MinerIdentifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

/**
 * Result of parsing the text decoded from a scanned miner label. `type`
 * records what the value looks like so the server can route the lookup;
 * `UNSPECIFIED` means "couldn't tell — let the server infer".
 */
export interface ScannedIdentifier {
  value: string;
  type: MinerIdentifierType;
}

// Leading label token for a serial (SN, S/N, SERIAL, SERIAL NO/NUMBER)
// followed by a *required* separator — punctuation (`:`, `=`, `#`) or
// whitespace. Requiring the separator keeps a serial that legitimately begins
// with the letters "SN" (e.g. "SNX9000") from being truncated.
const SERIAL_PREFIX = /^\s*(?:s\/?n|serial(?:\s*(?:no|number))?)(?:\s*[:=#]\s*|\s+)/i;

// Leading label token for a MAC (MAC, MAC ADDRESS) followed by the same
// required separator.
const MAC_PREFIX = /^\s*mac(?:\s*address)?(?:\s*[:=#]\s*|\s+)/i;

// A MAC address after prefix stripping: six hex pairs separated by ':' or '-',
// or twelve bare hex digits. Case-insensitive.
const MAC_VALUE = /^(?:[0-9a-f]{2}([:-])){5}[0-9a-f]{2}$|^[0-9a-f]{12}$/i;

/**
 * Extract a miner identifier from raw scanned text.
 *
 * Handles both label formats observed in the field:
 *   - `SN:1234567890123456`      → serial
 *   - `MAC:00:1A:2B:3C:4D:5E`    → MAC
 * plus prefix-less payloads, where the value's shape decides the type. When
 * nothing usable remains, returns an empty value with UNSPECIFIED type
 * (callers treat that as "not an identifier").
 */
export function parseScannedIdentifier(raw: string): ScannedIdentifier {
  if (!raw) return { value: "", type: MinerIdentifierType.UNSPECIFIED };

  // Collapse to the first line — scanners may append CR/LF, and multi-line
  // payloads (rare) put the identifier first.
  const firstLine = raw.split(/[\r\n]/, 1)[0] ?? "";

  // Explicit MAC prefix wins outright.
  if (MAC_PREFIX.test(firstLine)) {
    return { value: firstLine.replace(MAC_PREFIX, "").trim(), type: MinerIdentifierType.MAC_ADDRESS };
  }

  // Explicit serial prefix.
  if (SERIAL_PREFIX.test(firstLine)) {
    return { value: firstLine.replace(SERIAL_PREFIX, "").trim(), type: MinerIdentifierType.SERIAL_NUMBER };
  }

  // No discriminating prefix — infer from the value's shape.
  const value = firstLine.trim();
  if (MAC_VALUE.test(value)) {
    return { value, type: MinerIdentifierType.MAC_ADDRESS };
  }
  return { value, type: MinerIdentifierType.UNSPECIFIED };
}
