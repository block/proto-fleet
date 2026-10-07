import clsx from "clsx";
import { getNodeDisplayStatus } from "./getNodeDisplayStatus";
import type { NodeDisplayStatus } from "./getNodeDisplayStatus";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

const DISPLAY_STATUS_CONFIG: Record<NodeDisplayStatus, { label: string; dotClass: string }> = {
  connected: { label: "Connected", dotClass: "bg-intent-success-fill" },
  heartbeatOnly: { label: "Heartbeat only", dotClass: "bg-intent-warning-fill" },
  upgradeRequired: { label: "Upgrade required", dotClass: "bg-intent-critical-fill" },
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
