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
  totalCount: number;
  noDataElement?: React.ReactNode;
}

const ActivityTable = ({ activities, totalCount, noDataElement }: ActivityTableProps) => {
  const [selectedEntry, setSelectedEntry] = useState<ActivityEntry | null>(null);
  const handleDismiss = useCallback(() => setSelectedEntry(null), []);
  const grouped = useMemo(() => groupActivities(activities), [activities]);
  const hiddenCount = activities.length - grouped.length;
  const displayCount = totalCount - hiddenCount;

  return (
    <div>
      <div className="px-4 py-2 text-200 text-text-primary-50">
        {displayCount} {displayCount === 1 ? "activity" : "activities"}
      </div>
      <div className="divide-y divide-surface-10">
        {grouped.length === 0 && (noDataElement ?? defaultNoDataElement)}
        {grouped.map((entry) => {
          const isFailed = entry.result === "failure";
          const Icon = isFailed ? Alert : getActivityIcon(entry.eventType);

          return (
            <div
              key={entry.eventId}
              className="grid cursor-pointer grid-cols-[1fr_12rem_10rem_10rem] items-start gap-4 px-4 py-3 hover:bg-surface-5"
              onClick={() => setSelectedEntry(entry)}
            >
              <div className="flex items-start gap-2">
                <div className={clsx("shrink-0", isFailed ? "text-intent-critical" : "text-text-primary")}>
                  <Icon width="w-4" />
                </div>
                <span className="min-w-0 break-words">
                  {isCompletedEvent(entry.eventType)
                    ? entry.description.replace(/\s*completed\s*/i, " ").trim()
                    : entry.description}
                </span>
                {isFailed && <span className="text-intent-critical shrink-0 text-200">Failed</span>}
              </div>
              <div className="text-text-primary">
                {formatScope(entry.scopeType, entry.scopeLabel, entry.scopeCount || undefined)}
              </div>
              <div className="text-text-primary">{entry.username ?? "—"}</div>
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
