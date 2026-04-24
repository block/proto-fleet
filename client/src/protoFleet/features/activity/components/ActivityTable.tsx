import React, { useMemo } from "react";
import clsx from "clsx";

import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { getActivityIcon } from "@/protoFleet/features/activity/utils/activityIcons";
import { formatScope } from "@/protoFleet/features/activity/utils/formatScope";
import { Alert } from "@/shared/assets/icons";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import { formatActivityTimestamp } from "@/shared/utils/formatTimestamp";

type ActivityColumns = "type" | "scope" | "user" | "timestamp";

const colTitles: ColTitles<ActivityColumns> = {
  type: "Type",
  scope: "Scope",
  user: "User",
  timestamp: "Timestamp",
};

const activeCols: ActivityColumns[] = ["type", "scope", "user", "timestamp"];

const defaultNoDataElement = <div className="py-10 text-center text-text-primary-50">No activity to display.</div>;

interface ActivityTableProps {
  activities: ActivityEntry[];
  totalCount: number;
  noDataElement?: React.ReactNode;
}

const ActivityTable = ({ activities, totalCount, noDataElement }: ActivityTableProps) => {
  const colConfig: ColConfig<ActivityEntry, string, ActivityColumns> = useMemo(
    () => ({
      type: {
        component: (entry) => {
          const isFailed = entry.result === "failure";
          const Icon = isFailed ? Alert : getActivityIcon(entry.eventType);
          return (
            <div className="flex items-start gap-2">
              <div className={clsx("shrink-0", isFailed ? "text-intent-critical" : "text-text-primary")}>
                <Icon width="w-4" />
              </div>
              <span className="min-w-0 break-words">{entry.description}</span>
              {isFailed ? <span className="text-intent-critical shrink-0 text-200">Failed</span> : null}
            </div>
          );
        },
        width: "min-w-80",
        allowWrap: true,
      },
      scope: {
        component: (entry) => (
          <span>{formatScope(entry.scopeType, entry.scopeLabel, entry.scopeCount || undefined)}</span>
        ),
        width: "w-48",
      },
      user: {
        component: (entry) => <span>{entry.username ?? "\u2014"}</span>,
        width: "w-40",
      },
      timestamp: {
        component: (entry) => <span>{formatActivityTimestamp(Number(entry.createdAt?.seconds))}</span>,
        width: "w-40",
      },
    }),
    [],
  );

  return (
    <List<ActivityEntry, string, ActivityColumns>
      items={activities}
      itemKey="eventId"
      activeCols={activeCols}
      colTitles={colTitles}
      colConfig={colConfig}
      total={totalCount}
      stickyFirstColumn={false}
      itemName={{ singular: "activity", plural: "activities" }}
      noDataElement={noDataElement ?? defaultNoDataElement}
    />
  );
};

export default ActivityTable;
