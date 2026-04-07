import clsx from "clsx";

import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import {
  scheduleActionLabels,
  scheduleCols,
  type ScheduleColumn,
  scheduleStatusDotClassName,
  scheduleStatusLabels,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import { Grip } from "@/shared/assets/icons";
import type { ColConfig } from "@/shared/components/List/types";

const createScheduleColConfig = (): ColConfig<ScheduleListItem, string, ScheduleColumn> => ({
  [scheduleCols.priority]: {
    component: () => (
      <div className="flex items-center justify-center text-text-primary-50">
        <Grip width="w-4" className="h-4 shrink-0" />
      </div>
    ),
    width: "w-[5%] phone:w-auto",
  },
  [scheduleCols.name]: {
    component: (schedule) => (
      <div className="flex min-w-0 flex-col gap-1">
        <span className="truncate text-emphasis-300 text-text-primary">{schedule.name}</span>
        <span className="truncate text-200 text-text-primary-70">{schedule.targetSummary}</span>
      </div>
    ),
    width: "w-[18%] phone:w-auto",
  },
  [scheduleCols.schedule]: {
    component: (schedule) => (
      <div className="flex min-w-0 flex-col gap-1">
        <span className="truncate text-text-primary">{schedule.scheduleSummary}</span>
        <span className="truncate text-200 text-text-primary-70">{schedule.nextRunSummary ?? "—"}</span>
      </div>
    ),
    width: "w-[30%] phone:w-auto",
  },
  [scheduleCols.action]: {
    component: (schedule) => <span>{scheduleActionLabels[schedule.action]}</span>,
    width: "w-[14%] phone:w-auto",
  },
  [scheduleCols.status]: {
    component: (schedule) => (
      <div className="flex items-center gap-2">
        <span className={clsx("h-2 w-2 rounded-full", scheduleStatusDotClassName[schedule.status])} />
        <span>{scheduleStatusLabels[schedule.status]}</span>
      </div>
    ),
    width: "w-[12%] phone:w-auto",
  },
  [scheduleCols.createdBy]: {
    component: (schedule) => <span>{schedule.createdBy}</span>,
    width: "w-[14%] phone:w-auto",
  },
});

export default createScheduleColConfig;
