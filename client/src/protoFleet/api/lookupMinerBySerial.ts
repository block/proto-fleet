import { Code, ConnectError } from "@connectrpc/connect";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

/**
 * Outcome of a serial-number lookup. Callers switch on `status` so the scan
 * UI can distinguish "this serial isn't a paired miner" (an expected, common
 * case worth a friendly message) from an unexpected transport/server error.
 */
export type LookupMinerResult =
  | { status: "found"; snapshot: MinerStateSnapshot }
  | { status: "notFound" }
  | { status: "error"; message: string };

/**
 * Resolve a single paired miner from an exact serial number via
 * FleetManagementService.LookupMinerBySerialNumber.
 *
 * The `serial` passed here must already be the bare value (prefix-stripped,
 * trimmed) — see parseScannedSerial. An empty serial short-circuits to
 * notFound without a round-trip.
 */
export async function lookupMinerBySerial(serial: string, signal?: AbortSignal): Promise<LookupMinerResult> {
  if (!serial) return { status: "notFound" };

  try {
    const response = await fleetManagementClient.lookupMinerBySerialNumber({ serialNumber: serial }, { signal });
    if (!response.snapshot) return { status: "notFound" };
    return { status: "found", snapshot: response.snapshot };
  } catch (err) {
    if (err instanceof ConnectError && err.code === Code.NotFound) {
      return { status: "notFound" };
    }
    return { status: "error", message: getErrorMessage(err, "Failed to look up miner. Please try again.") };
  }
}
