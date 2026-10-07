import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

// Four missed heartbeats matches the miner availability and alert thresholds.
const STALE_AFTER_MS = 120_000;

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
      if (!node.lastSeenAt) return "neverConnected";
      if (Date.now() - node.lastSeenAt.getTime() > STALE_AFTER_MS) return "stale";
      return node.controlStreamConnected ? "connected" : "heartbeatOnly";
    case FleetNodeEnrollmentStatus.REVOKED:
      return "revoked";
    default:
      return "pending";
  }
};
