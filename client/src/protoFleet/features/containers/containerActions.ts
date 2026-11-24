import { awaitEquipmentCommand } from "./awaitEquipmentCommand";
import { containersClient } from "@/protoFleet/api/clients";

// Native controller operations retain the existing MDK v2 menu semantics.
// Never resolve these into module identifiers: the controller owns dispatch.
export const CONTAINER_ACTIONS = [
  { label: "Sleep container", path: "mining/stop", permission: "miner:stop_mining", kind: "command" },
  { label: "Wake container", path: "mining/start", permission: "miner:start_mining", kind: "command" },
  { label: "Manage power", path: "mining/target-power", permission: "miner:set_power_target", kind: "power" },
] as const;

export type ContainerAction = Omit<(typeof CONTAINER_ACTIONS)[number], "label"> & { label: string };

// The subset of MDK v2 responses consumed by the Fleet action dialogs.
export interface ContainerActionResponse {
  mining?: boolean;
  operatingState?: string;
  watts?: number;
  minimumWatts?: number;
  maximumWatts?: number;
  performanceMode?: string;
  balanceBays?: boolean;
  hashOnDisconnect?: boolean;
}

export async function requestContainerAction(
  deviceIdentifier: string,
  path: ContainerAction["path"] | "mining/status",
  method: "GET" | "POST" | "PUT",
  body?: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<ContainerActionResponse> {
  const options = { signal, timeoutMs: 10_000 };
  if (method === "GET" && path === "mining/status") {
    return containersClient.getMiningStatus({ deviceIdentifier }, options);
  }
  if (method === "GET" && path === "mining/target-power") {
    return containersClient.getPowerTarget({ deviceIdentifier }, options);
  }
  let batchIdentifier: string;
  if (method === "POST" && (path === "mining/start" || path === "mining/stop")) {
    ({ batchIdentifier } = await containersClient.setMining(
      { deviceIdentifier, mining: path === "mining/start" },
      options,
    ));
  } else if (method === "PUT" && path === "mining/target-power") {
    if (typeof body?.watts !== "number") throw new Error("Power target is required.");
    ({ batchIdentifier } = await containersClient.setPowerTarget(
      {
        deviceIdentifier,
        watts: body.watts,
        performanceMode: typeof body.performanceMode === "string" ? body.performanceMode : "",
        balanceBays: typeof body.balanceBays === "boolean" ? body.balanceBays : undefined,
        hashOnDisconnect: typeof body.hashOnDisconnect === "boolean" ? body.hashOnDisconnect : undefined,
      },
      options,
    ));
  } else {
    throw new Error("Unsupported container action.");
  }
  if (!batchIdentifier) throw new Error("Command outcome unconfirmed. Refresh before retrying.");
  await awaitEquipmentCommand(deviceIdentifier, batchIdentifier, signal);
  if (path === "mining/target-power") {
    // A failed refresh cannot turn an acknowledged write into a retry.
    try {
      return await containersClient.getPowerTarget({ deviceIdentifier }, options);
    } catch {
      return {};
    }
  }
  return {};
}

export function containerMiningCommand(status: ContainerActionResponse): "mining/start" | "mining/stop" | undefined {
  switch (status.operatingState) {
    case "MINING_OPERATING_STATE_STOPPED":
      return "mining/start";
    case "MINING_OPERATING_STATE_MINING":
    case "MINING_OPERATING_STATE_DEGRADED_MINING":
    case "MINING_OPERATING_STATE_NO_POOLS":
    case "MINING_OPERATING_STATE_ERROR":
      return "mining/stop";
    case "MINING_OPERATING_STATE_POWERING_ON":
    case "MINING_OPERATING_STATE_POWERING_OFF":
      return undefined;
    default:
      return typeof status.mining === "boolean" ? (status.mining ? "mining/stop" : "mining/start") : undefined;
  }
}
