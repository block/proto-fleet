import clsx from "clsx";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

// The daemon heartbeats every 30s (server/cmd/fleetnode/run.go); allow two
// missed beats before calling a node stale. The server only records
// last_seen_at, so connectivity is derived client-side.
const STALE_AFTER_MS = 90_000;

type NodeDisplayStatus = "online" | "stale" | "neverConnected" | "awaitingConfirmation" | "pending" | "revoked";

const getNodeDisplayStatus = (node: FleetNodeItem): NodeDisplayStatus => {
  switch (node.enrollmentStatus) {
    case FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION:
      return "awaitingConfirmation";
    case FleetNodeEnrollmentStatus.CONFIRMED:
      if (!node.lastSeenAt) return "neverConnected";
      return Date.now() - node.lastSeenAt.getTime() <= STALE_AFTER_MS ? "online" : "stale";
    case FleetNodeEnrollmentStatus.REVOKED:
      return "revoked";
    default:
      return "pending";
  }
};

const DISPLAY_STATUS_CONFIG: Record<NodeDisplayStatus, { label: string; dotClass: string }> = {
  online: { label: "Online", dotClass: "bg-intent-success-fill" },
  stale: { label: "Stale", dotClass: "bg-intent-warning-fill" },
  neverConnected: { label: "Never connected", dotClass: "bg-border-20" },
  awaitingConfirmation: { label: "Awaiting confirmation", dotClass: "bg-intent-info-fill" },
  pending: { label: "Pending", dotClass: "bg-border-20" },
  revoked: { label: "Revoked", dotClass: "bg-intent-critical-fill" },
};

const NodeStatusBadge = ({ node }: { node: FleetNodeItem }) => {
  const status = getNodeDisplayStatus(node);
  const config = DISPLAY_STATUS_CONFIG[status];
  return (
    <span className="inline-flex items-center gap-2 text-300 text-text-primary-50">
      <span className={clsx("h-2 w-2 rounded-full", config.dotClass)} />
      {config.label}
    </span>
  );
};

export default NodeStatusBadge;
