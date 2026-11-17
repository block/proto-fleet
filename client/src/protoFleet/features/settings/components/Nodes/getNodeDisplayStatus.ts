import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

// Four missed heartbeats matches the miner availability and alert thresholds.
export const STALE_AFTER_MS = 120_000;

export type NodeDisplayStatus =
  | "connected"
  | "heartbeatOnly"
  | "upgradeRequired"
  | "stale"
  | "neverConnected"
  | "awaitingConfirmation"
  | "pending"
  | "revoked";

export const getNodeDisplayStatus = (node: FleetNodeItem): NodeDisplayStatus => {
  switch (node.enrollmentStatus) {
    case FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION:
      return "awaitingConfirmation";
    case FleetNodeEnrollmentStatus.CONFIRMED:
      if (node.commandProtocolUpgradeRequired && node.controlStreamConnected) return "upgradeRequired";
      if (node.lastSeenAt && Date.now() - node.lastSeenAt.getTime() > STALE_AFTER_MS) return "stale";
      if (!node.lastSeenAt) return "neverConnected";
      return node.controlStreamConnected ? "connected" : "heartbeatOnly";
    case FleetNodeEnrollmentStatus.REVOKED:
      return "revoked";
    default:
      return "pending";
  }
};

export type NodeUpgradeStatus = "required" | "recommended";

// The server only reports protocol upgrades for a live control connection, so
// a disconnected node's last-known version carries no upgrade state.
export const getNodeUpgradeStatus = (node: FleetNodeItem): NodeUpgradeStatus | null => {
  if (node.enrollmentStatus !== FleetNodeEnrollmentStatus.CONFIRMED || !node.controlStreamConnected) return null;
  if (node.commandProtocolUpgradeRequired) return "required";
  return node.commandProtocolUpgradeRecommended ? "recommended" : null;
};
