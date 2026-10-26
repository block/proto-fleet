import { API_PROXY_BASE } from "@/protoFleet/api/constants";

// Container-wide operations from proto-rig-api/openapi/MDK-v2-API.json.
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
  const response = await fetch(`${API_PROXY_BASE}/miners/${encodeURIComponent(deviceIdentifier)}/api/v2/${path}`, {
    method,
    credentials: "include",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    signal,
  });
  const data = await response.json().catch(() => undefined);
  if (!response.ok) {
    throw new Error(
      typeof data?.message === "string"
        ? data.message
        : typeof data?.error === "string"
          ? data.error
          : `Container request failed (${response.status}).`,
    );
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error("Invalid response from the container.");
  return data;
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
