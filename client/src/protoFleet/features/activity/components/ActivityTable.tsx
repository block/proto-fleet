import React, { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import ActivityDetailModal from "@/protoFleet/features/activity/components/ActivityDetailModal";
import { getActivityIcon } from "@/protoFleet/features/activity/utils/activityIcons";
import { isCompletedEvent } from "@/protoFleet/features/activity/utils/eventType";
import { formatScope } from "@/protoFleet/features/activity/utils/formatScope";
import { Alert } from "@/shared/assets/icons";
import { formatActivityTimestamp } from "@/shared/utils/formatTimestamp";

const defaultNoDataElement = <div className="py-10 text-center text-text-primary-50">No activity to display.</div>;

/**
 * When a batch completes, the backend writes both an initiated row (e.g. "reboot")
 * and a completed row ("reboot.completed"). We collapse them into one entry by
 * keeping the completed row and hiding the initiated row for the same batch_id.
 */
function groupActivities(activities: ActivityEntry[]): ActivityEntry[] {
  const completedBatchIds = new Set<string>();
  for (const entry of activities) {
    if (entry.batchId && isCompletedEvent(entry.eventType)) {
      completedBatchIds.add(entry.batchId);
    }
  }
  return activities.filter((entry) => {
    if (!entry.batchId) return true;
    if (isCompletedEvent(entry.eventType)) return true;
    return !completedBatchIds.has(entry.batchId);
  });
}

interface ActivityTableProps {
  activities: ActivityEntry[];
  noDataElement?: React.ReactNode;
}

const ActivityTable = ({ activities, noDataElement }: ActivityTableProps) => {
  const [selectedEntry, setSelectedEntry] = useState<ActivityEntry | null>(null);
  const handleDismiss = useCallback(() => setSelectedEntry(null), []);
  const grouped = useMemo(() => groupActivities(activities), [activities]);

  return (
    <div>
      <div className="px-4 py-2 text-200 text-text-primary-50">
        {grouped.length} {grouped.length === 1 ? "activity" : "activities"}
      </div>
      <div className="divide-y divide-surface-10">
        {grouped.length === 0 ? (noDataElement ?? defaultNoDataElement) : null}
        {grouped.map((entry) => {
          const isFailed = entry.result === "failure";
          const Icon = isFailed ? Alert : getActivityIcon(entry.eventType);

          return (
            <div
              key={entry.eventId}
              role="button"
              tabIndex={0}
              data-testid="list-row"
              className="grid cursor-pointer grid-cols-[1fr_12rem_10rem_13rem] items-start gap-4 px-4 py-3 hover:bg-surface-5"
              onClick={() => setSelectedEntry(entry)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  setSelectedEntry(entry);
                }
              }}
            >
              <div data-testid="type" className="flex items-start gap-2">
                <div className={clsx("shrink-0", isFailed ? "text-intent-critical" : "text-text-primary")}>
                  <Icon width="w-4" />
                  {isFailed ? <span className="sr-only">Failed</span> : null}
                </div>
                <span className="min-w-0 break-words">
                  {isCompletedEvent(entry.eventType)
                    ? entry.description.replace(/\s*completed\s*/i, " ").trim()
                    : entry.description}
                </span>
              </div>
              <div data-testid="scope" className="text-text-primary">
                {formatScope(entry.scopeType, entry.scopeLabel, entry.scopeCount || undefined)}
              </div>
              <div data-testid="user" className="text-text-primary">
                {entry.username ?? "—"}
              </div>
              <div className="text-text-primary">{formatActivityTimestamp(Number(entry.createdAt?.seconds))}</div>
            </div>
          );
        })}
      </div>

      <ActivityDetailModal entry={selectedEntry} onDismiss={handleDismiss} />
    </div>
  );
};

export default ActivityTable;
