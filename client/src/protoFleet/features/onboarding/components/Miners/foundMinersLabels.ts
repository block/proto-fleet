import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";

export const FLEET_SERVER_ID = "0";
export const RECOMMENDED_MINERS_PER_NODE = 4000;
export const MAX_MINERS_PER_NODE = 5000;

/** Fleet Server for node id 0, otherwise the node's name or "Fleet Node <id>" when the name is unknown. */
export function fleetSourceLabel(fleetNodeId: string, name?: string): string {
  return fleetNodeId === FLEET_SERVER_ID ? "Fleet Server" : name || `Fleet Node ${fleetNodeId}`;
}

/** Entity noun for a group's count, pluralized: "container(s)" for containers, "miner(s)" otherwise. */
export function entityNoun(deviceClass: DeviceClass, count: number): string {
  if (deviceClass === DeviceClass.CONTAINER) return count === 1 ? "container" : "containers";
  return count === 1 ? "miner" : "miners";
}

/**
 * Display label for a discovered device group.
 * - Containers are labelled "Proto Container".
 * - Otherwise the manufacturer and model are joined ("Bitmain Antminer S21"),
 *   but a redundant manufacturer prefix is dropped when the model already leads
 *   with it as a whole word (manufacturer "Proto" + model "Proto Rig" -> "Proto Rig").
 */
export function deviceGroupLabel(manufacturer: string, model: string, deviceClass: DeviceClass): string {
  if (deviceClass === DeviceClass.CONTAINER) return "Proto Container";

  const mfr = (manufacturer ?? "").trim();
  const mdl = (model ?? "").trim();
  if (!mfr) return mdl;
  if (!mdl) return mfr;

  const mdlLower = mdl.toLowerCase();
  const mfrLower = mfr.toLowerCase();
  // Drop the prefix only on a whole-word match so "Proto" doesn't swallow "Protocol".
  if (mdlLower === mfrLower || mdlLower.startsWith(mfrLower + " ")) return mdl;

  return `${mfr} ${mdl}`;
}

/**
 * Header summary, splitting the total by entity, e.g.
 * "48 containers and 300 miners found on your network". A bucket with a zero
 * count is omitted, so a rig-only scan keeps the original
 * "N miners found on your network" wording.
 */
export function foundSummary(containerCount: number, minerCount: number): string {
  const parts: string[] = [];
  if (containerCount > 0) parts.push(`${containerCount} ${entityNoun(DeviceClass.CONTAINER, containerCount)}`);
  if (minerCount > 0) parts.push(`${minerCount} ${entityNoun(DeviceClass.RIG, minerCount)}`);
  return `${parts.join(" and ")} found on your network`;
}
