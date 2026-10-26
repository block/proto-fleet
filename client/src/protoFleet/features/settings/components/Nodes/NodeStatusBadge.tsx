import clsx from "clsx";
import { getNodeDisplayStatus, STALE_AFTER_MS } from "./getNodeDisplayStatus";
import type { NodeDisplayStatus } from "./getNodeDisplayStatus";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

const DISPLAY_STATUS_CONFIG: Record<NodeDisplayStatus, { label: string; dotClass: string; description?: string }> = {
  connected: { label: "Connected", dotClass: "bg-intent-success-fill" },
  heartbeatOnly: {
    label: "Heartbeat only",
    dotClass: "bg-intent-warning-fill",
    description: "Recent heartbeat received, but no command connection is available.",
  },
  upgradeRequired: { label: "Upgrade required", dotClass: "bg-intent-critical-fill" },
  upgradeRecommended: { label: "Upgrade recommended", dotClass: "bg-intent-warning-fill" },
  stale: {
    label: "Stale",
    dotClass: "bg-intent-warning-fill",
    description: `No heartbeat received in the last ${STALE_AFTER_MS / 60_000} minutes.`,
  },
  neverConnected: { label: "Never connected", dotClass: "bg-border-20" },
  awaitingConfirmation: { label: "Awaiting confirmation", dotClass: "bg-intent-info-fill" },
  pending: { label: "Pending", dotClass: "bg-border-20" },
  revoked: { label: "Revoked", dotClass: "bg-intent-critical-fill" },
};

const NodeStatusBadge = ({ node }: { node: FleetNodeItem }) => {
  const status = getNodeDisplayStatus(node);
  const config = DISPLAY_STATUS_CONFIG[status];
  return (
    <span title={config.description} className="inline-flex flex-wrap items-center gap-2 text-300 text-text-primary-50">
      <span className={clsx("h-2 w-2 rounded-full", config.dotClass)} />
      <span>{config.label}</span>
      {config.description ? <span className="sr-only">{config.description}</span> : null}
    </span>
  );
};

export default NodeStatusBadge;
