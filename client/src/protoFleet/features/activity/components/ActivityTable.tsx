import React, { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { useCommandBatchDeviceResults } from "@/protoFleet/api/useCommandBatchDeviceResults";
import ActivityBatchDetails from "@/protoFleet/features/activity/components/ActivityBatchDetails";
import { getActivityIcon } from "@/protoFleet/features/activity/utils/activityIcons";
import { formatScope } from "@/protoFleet/features/activity/utils/formatScope";
import { Alert, ChevronDown } from "@/shared/assets/icons";
import { formatActivityTimestamp } from "@/shared/utils/formatTimestamp";

const defaultNoDataElement = <div className="py-10 text-center text-text-primary-50">No activity to display.</div>;

function isCompletedEvent(eventType: string): boolean {
  return eventType.endsWith(".completed");
}

function groupActivities(activities: ActivityEntry[]): ActivityEntry[] {
  const result: ActivityEntry[] = [];
  const hiddenBatchIds = new Set<string>();
  for (const entry of activities) {
    if (!entry.batchId) {
      result.push(entry);
      continue;
    }
    if (isCompletedEvent(entry.eventType)) {
      result.push(entry);
      hiddenBatchIds.add(entry.batchId);
      continue;
    }
    if (hiddenBatchIds.has(entry.batchId)) {
      continue;
    }
    result.push(entry);
  }
  return result;
}

interface ActivityTableProps {
  activities: ActivityEntry[];
  totalCount: number;
  noDataElement?: React.ReactNode;
}

const ActivityTable = ({ activities, totalCount, noDataElement }: ActivityTableProps) => {
  const [expandedBatchIds, setExpandedBatchIds] = useState<Set<string>>(new Set());
  const { fetch, getResult } = useCommandBatchDeviceResults();

  const toggleExpand = useCallback(
    (batchId: string) => {
      setExpandedBatchIds((prev) => {
        const next = new Set(prev);
        if (next.has(batchId)) {
          next.delete(batchId);
        } else {
          next.add(batchId);
          const cached = getResult(batchId);
          if (!cached.data && !cached.isLoading) {
            void fetch(batchId);
          }
        }
        return next;
      });
    },
    [fetch, getResult],
  );

  const grouped = useMemo(() => groupActivities(activities), [activities]);

  return (
    <div>
      <div className="px-4 py-2 text-200 text-text-primary-50">
        {totalCount} {totalCount === 1 ? "activity" : "activities"}
      </div>
      <div className="divide-y divide-surface-10">
        {grouped.length === 0 && (noDataElement ?? defaultNoDataElement)}
        {grouped.map((entry) => {
          const hasBatch = !!entry.batchId;
          const isExpanded = hasBatch && expandedBatchIds.has(entry.batchId!);
          const isFailed = entry.result === "failure";
          const isCompleted = isCompletedEvent(entry.eventType);
          const Icon = isFailed
            ? Alert
            : getActivityIcon(isCompleted ? entry.eventType.replace(".completed", "") : entry.eventType);

          return (
            <div key={entry.eventId}>
              <div
                className={clsx(
                  "grid grid-cols-[1fr_12rem_10rem_10rem] items-start gap-4 px-4 py-3",
                  hasBatch && "cursor-pointer hover:bg-surface-5",
                )}
                onClick={hasBatch ? () => toggleExpand(entry.batchId!) : undefined}
              >
                <div className="flex items-start gap-2">
                  {hasBatch && (
                    <div
                      className={clsx("shrink-0 text-text-primary-50 transition-transform", isExpanded && "rotate-180")}
                    >
                      <ChevronDown width="w-4" />
                    </div>
                  )}
                  <div className={clsx("shrink-0", isFailed ? "text-intent-critical" : "text-text-primary")}>
                    <Icon width="w-4" />
                  </div>
                  <span className="min-w-0 break-words">{entry.description}</span>
                  {isFailed && <span className="text-intent-critical shrink-0 text-200">Failed</span>}
                </div>
                <div className="text-text-primary">
                  {formatScope(entry.scopeType, entry.scopeLabel, entry.scopeCount || undefined)}
                </div>
                <div className="text-text-primary">{entry.username ?? "—"}</div>
                <div className="text-text-primary">{formatActivityTimestamp(Number(entry.createdAt?.seconds))}</div>
              </div>
              {isExpanded && entry.batchId && (
                <div className="bg-surface-5 px-4 pb-3 pl-14">
                  <ActivityBatchDetails batchId={entry.batchId} {...getResult(entry.batchId)} />
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default ActivityTable;
