import { Code, ConnectError } from "@connectrpc/connect";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import type {
  MinerIdentifierType,
  MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

/**
 * Outcome of an identifier lookup. Callers switch on `status` so the scan UI
 * can distinguish "this identifier isn't a paired miner" (an expected, common
 * case worth a friendly message) from an unexpected transport/server error.
 */
export type LookupMinerResult =
  { status: "found"; snapshot: MinerStateSnapshot } | { status: "notFound" } | { status: "error"; message: string };

/**
 * Resolve a single paired miner from a scanned identifier (MAC or serial) via
 * FleetManagementService.LookupMinerByIdentifier.
 *
 * `identifier` must already be the bare value (prefix-stripped, trimmed) — see
 * parseScannedIdentifier. `type` tells the server how to interpret it; pass
 * UNSPECIFIED to let the server infer from the value shape. An empty
 * identifier short-circuits to notFound without a round-trip.
 */
export async function lookupMinerByIdentifier(
  identifier: string,
  type: MinerIdentifierType,
  signal?: AbortSignal,
): Promise<LookupMinerResult> {
  if (!identifier) return { status: "notFound" };

  try {
    const response = await fleetManagementClient.lookupMinerByIdentifier(
      { identifier, identifierType: type },
      { signal },
    );
    if (!response.snapshot) return { status: "notFound" };
    return { status: "found", snapshot: response.snapshot };
  } catch (err) {
    if (err instanceof ConnectError && err.code === Code.NotFound) {
      return { status: "notFound" };
    }
    return { status: "error", message: getErrorMessage(err, "Failed to look up miner. Please try again.") };
  }
}
