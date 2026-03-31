import clsx from "clsx";

import type { ScheduleListItem, ScheduleStatus } from "@/protoFleet/api/useScheduleApi";
import {
  scheduleActionLabels,
  scheduleCols,
  type ScheduleColumn,
  scheduleStatusLabels,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import { Grip } from "@/shared/assets/icons";
import type { ColConfig } from "@/shared/components/List/types";

const scheduleStatusDotClassName: Record<ScheduleStatus, string> = {
  running: "bg-intent-success-fill",
  active: "bg-intent-success-fill",
  paused: "bg-text-primary-30",
  completed: "bg-text-primary-30",
};

const createScheduleColConfig = (): ColConfig<ScheduleListItem, string, ScheduleColumn> => ({
  [scheduleCols.priority]: {
    component: () => (
      <div className="flex items-center justify-center text-text-primary-50">
        <Grip width="w-4" />
      </div>
    ),
    width: "w-12",
  },
  [scheduleCols.name]: {
    component: (schedule) => (
      <div className="flex min-w-0 flex-col gap-1">
        <span className="truncate text-emphasis-300 text-text-primary">{schedule.name}</span>
        <span className="truncate text-200 text-text-primary-70">{schedule.targetSummary}</span>
      </div>
    ),
    width: "w-72",
  },
  [scheduleCols.schedule]: {
    component: (schedule) => (
      <div className="flex min-w-0 flex-col gap-1">
        <span className="truncate text-text-primary">{schedule.scheduleSummary}</span>
        <span className="truncate text-200 text-text-primary-70">{schedule.nextRunSummary ?? "—"}</span>
      </div>
    ),
    width: "w-80",
  },
  [scheduleCols.action]: {
    component: (schedule) => <span>{scheduleActionLabels[schedule.action]}</span>,
    width: "w-44",
  },
  [scheduleCols.status]: {
    component: (schedule) => (
      <div className="flex items-center gap-2">
        <span className={clsx("h-2 w-2 rounded-full", scheduleStatusDotClassName[schedule.status])} />
        <span>{scheduleStatusLabels[schedule.status]}</span>
      </div>
    ),
    width: "w-36",
  },
  [scheduleCols.createdBy]: {
    component: (schedule) => <span>{schedule.createdBy}</span>,
    width: "w-44",
  },
});

export default createScheduleColConfig;
