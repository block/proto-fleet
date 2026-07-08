import { type ReactNode, useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import ActivityDetailModal from "@/protoFleet/features/activity/components/ActivityDetailModal";
import { getActivityIcon, getActivityIconTone } from "@/protoFleet/features/activity/utils/activityIcons";
import { isCompletedEvent } from "@/protoFleet/features/activity/utils/eventType";
import { formatActivityDescription } from "@/protoFleet/features/activity/utils/formatActivityDescription";
import { formatScope } from "@/protoFleet/features/activity/utils/formatScope";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";
import { formatActivityTimestamp } from "@/shared/utils/formatTimestamp";

const defaultNoDataElement = <div className="py-10 text-center text-text-primary-50">No activity to display.</div>;

type ActivityColumn = "type" | "scope" | "user" | "time";

const activeActivityColumns: ActivityColumn[] = ["type", "scope", "user", "time"];

const activityColumnTitles: ColTitles<ActivityColumn> = {
  type: "Activity",
  scope: "Scope",
  user: "User",
  time: "Time",
};

const activityTableClassName = "mb-0 w-full phone:table-fixed";

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
  noDataElement?: ReactNode;
}

function ActivityTypeCell({ entry }: { entry: ActivityEntry }) {
  const icon = getActivityIcon(
    entry.eventType,
    entry.result,
  )({
    width: "h-5 w-5",
    className: "[&_svg]:h-full [&_svg]:w-full",
  });
  const iconTone = getActivityIconTone(entry.eventType, entry.result);

  return (
    <div className="flex min-w-0 items-start gap-3">
      <div
        className={clsx(
          "mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center",
          iconTone === "critical" ? "text-intent-critical-fill" : "text-text-primary",
        )}
      >
        {icon}
        {entry.result === "failure" ? <span className="sr-only">Couldn't complete</span> : null}
      </div>
      <span className="min-w-0 text-emphasis-300 break-words">{formatActivityDescription(entry)}</span>
    </div>
  );
}

const ActivityTable = ({ activities, noDataElement }: ActivityTableProps) => {
  const [selectedEntry, setSelectedEntry] = useState<ActivityEntry | null>(null);
  const handleDismiss = useCallback(() => setSelectedEntry(null), []);
  const grouped = useMemo(() => groupActivities(activities), [activities]);
  const colConfig: ColConfig<ActivityEntry, string, ActivityColumn> = useMemo(
    () => ({
      type: {
        component: (entry) => <ActivityTypeCell entry={entry} />,
        width: "w-[22rem] phone:w-[17rem]",
        allowWrap: true,
      },
      scope: {
        component: (entry) => formatScope(entry.scopeType, entry.scopeLabel, entry.scopeCount || undefined),
        width: "w-48",
      },
      user: {
        component: (entry) => entry.username ?? INACTIVE_PLACEHOLDER,
        width: "w-36",
      },
      time: {
        component: (entry) => formatActivityTimestamp(Number(entry.createdAt?.seconds)),
        width: "w-52",
      },
    }),
    [],
  );
  const handleRowClick = useCallback((entry: ActivityEntry) => setSelectedEntry(entry), []);

  return (
    <>
      <List<ActivityEntry, string, ActivityColumn>
        activeCols={activeActivityColumns}
        colTitles={activityColumnTitles}
        colConfig={colConfig}
        items={grouped}
        itemKey="eventId"
        total={grouped.length}
        itemName={{ singular: "activity", plural: "activities" }}
        noDataElement={noDataElement ?? defaultNoDataElement}
        onRowClick={handleRowClick}
        applyColumnWidthsToCells
        stickyFirstColumn={false}
        tableClassName={activityTableClassName}
      />
      <ActivityDetailModal entry={selectedEntry} onDismiss={handleDismiss} />
    </>
  );
};

export default ActivityTable;
