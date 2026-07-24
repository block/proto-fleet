import clsx from "clsx";

import type { AgentActivity } from "./types";
import { Checkmark, DismissTiny } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";

const STATUS_LABELS: Record<AgentActivity["status"], string> = {
  running: "In progress",
  completed: "Completed",
  failed: "Couldn't complete",
  cancelled: "Cancelled",
};

interface AgentActivityStatusProps {
  activity: AgentActivity;
}

const AgentActivityStatus = ({ activity }: AgentActivityStatusProps) => (
  <div
    className="flex min-w-0 items-center gap-2 py-1 text-200 text-text-primary-50"
    data-testid="agent-activity-status"
  >
    <span
      aria-label={STATUS_LABELS[activity.status]}
      className={clsx("flex size-4 shrink-0 items-center justify-center", {
        "text-text-success": activity.status === "completed",
        "text-intent-info-fill": activity.status === "running",
        "text-text-critical": activity.status === "failed",
        "text-text-primary-30": activity.status === "cancelled",
      })}
      role="img"
    >
      {activity.status === "completed" ? <Checkmark width="w-3.5" /> : null}
      {activity.status === "running" ? <ProgressCircular indeterminate size={14} /> : null}
      {activity.status === "failed" ? <DismissTiny width="w-3" /> : null}
      {activity.status === "cancelled" ? <DismissTiny width="w-3" /> : null}
    </span>
    <span className="min-w-0 break-words">{activity.summary}</span>
  </div>
);

export default AgentActivityStatus;
