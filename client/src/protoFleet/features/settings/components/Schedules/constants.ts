import { arrayMove } from "@dnd-kit/sortable";
import type { ScheduleAction, ScheduleListItem, ScheduleStatus } from "@/protoFleet/api/useScheduleApi";
import type { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import type { SortDirection } from "@/shared/components/List/types";

export const SCHEDULE_PAGE_DESCRIPTION =
  "Add schedules and order them by priority. When schedules conflict, the higher-priority schedule will be used. Outside of schedule windows, miners fall back to their default targets unless they are overridden from an individual miner's settings.";

export const SCHEDULE_EMPTY_STATE_DESCRIPTION = "Configure schedules to automate actions for your miners.";

export const scheduleCols = {
  priority: "priority",
  name: "name",
  schedule: "schedule",
  action: "action",
  status: "status",
  createdBy: "createdBy",
} as const;

export type ScheduleColumn = (typeof scheduleCols)[keyof typeof scheduleCols];

export const scheduleColTitles: Record<ScheduleColumn, string> = {
  priority: "",
  name: "Name",
  schedule: "Schedule",
  action: "Action",
  status: "Status",
  createdBy: "Created by",
};

export const scheduleColumnAriaLabels: Partial<Record<ScheduleColumn, string>> = {
  priority: "Reorder",
};

export const activeScheduleCols: ScheduleColumn[] = [
  scheduleCols.priority,
  scheduleCols.name,
  scheduleCols.schedule,
  scheduleCols.action,
  scheduleCols.status,
  scheduleCols.createdBy,
];

export const scheduleTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "phone:[&_td:last-child]:w-9",
  "phone:[&_th:last-child]:w-9",
  "phone:[&_td:last-child>div]:box-border",
  "phone:[&_td:last-child>div]:flex",
  "phone:[&_td:last-child>div]:justify-end",
  "phone:[&_td:last-child>div]:w-9",
  "phone:[&_th:last-child>div]:w-9",
].join(" ");

export const scheduleStatusLabels: Record<ScheduleStatus, string> = {
  running: "Running",
  active: "Active",
  paused: "Paused",
  completed: "Completed",
};

export const scheduleStatusDotClassName: Record<ScheduleStatus, string> = {
  running: "bg-intent-success-fill",
  active: "bg-intent-success-fill",
  paused: "bg-text-primary-30",
  completed: "bg-text-primary-30",
};

export const scheduleActionLabels: Record<ScheduleAction, string> = {
  setPowerTarget: "Set power target",
  reboot: "Reboot",
  sleep: "Sleep",
};

export const scheduleFilters: FilterItem[] = [
  {
    type: "dropdown",
    title: "Status",
    value: "status",
    showSelectAll: false,
    options: [
      { id: "running", label: "Running" },
      { id: "active", label: "Active" },
      { id: "paused", label: "Paused" },
      { id: "completed", label: "Completed" },
    ],
    defaultOptionIds: [],
  },
  {
    type: "dropdown",
    title: "Action",
    value: "action",
    showSelectAll: false,
    options: [
      { id: "setPowerTarget", label: "Set power target" },
      { id: "reboot", label: "Reboot" },
      { id: "sleep", label: "Sleep" },
    ],
    defaultOptionIds: [],
  },
];

export const SORTABLE_COLUMNS = new Set<ScheduleColumn>([
  scheduleCols.name,
  scheduleCols.action,
  scheduleCols.status,
  scheduleCols.createdBy,
]);

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });

const scheduleStatusOrder: Record<ScheduleStatus, number> = {
  running: 0,
  active: 1,
  paused: 2,
  completed: 3,
};

export const getDefaultScheduleSortDirection = (_field: ScheduleColumn): SortDirection => "asc";

export const sortSchedules = (
  schedules: ScheduleListItem[],
  currentSort?: { field: ScheduleColumn; direction: SortDirection },
): ScheduleListItem[] => {
  const directionMultiplier = currentSort?.direction === "desc" ? -1 : 1;

  return [...schedules].sort((left, right) => {
    if (!currentSort) {
      return left.priority - right.priority;
    }

    const comparison = (() => {
      switch (currentSort.field) {
        case scheduleCols.name:
          return collator.compare(left.name, right.name);
        case scheduleCols.action:
          return collator.compare(scheduleActionLabels[left.action], scheduleActionLabels[right.action]);
        case scheduleCols.status:
          return scheduleStatusOrder[left.status] - scheduleStatusOrder[right.status];
        case scheduleCols.createdBy:
          return collator.compare(left.createdBy, right.createdBy);
        default:
          return left.priority - right.priority;
      }
    })();

    if (comparison === 0) {
      return left.priority - right.priority;
    }

    return comparison * directionMultiplier;
  });
};

type ReorderScheduleIdsByDropArgs = {
  activeId: string;
  overId: string;
  visibleItemKeys: string[];
  priorityOrderedIds: string[];
};

export const reorderScheduleIdsByDrop = ({
  activeId,
  overId,
  visibleItemKeys,
  priorityOrderedIds,
}: ReorderScheduleIdsByDropArgs) => {
  const oldIndex = visibleItemKeys.indexOf(activeId);
  const newIndex = visibleItemKeys.indexOf(overId);

  if (oldIndex < 0 || newIndex < 0 || activeId === overId || oldIndex === newIndex) {
    return null;
  }

  const reorderedVisibleIds = arrayMove(visibleItemKeys, oldIndex, newIndex);

  if (visibleItemKeys.length === priorityOrderedIds.length) {
    return reorderedVisibleIds;
  }

  const visibleIdSet = new Set(visibleItemKeys);
  let reorderedVisibleIndex = 0;

  return priorityOrderedIds.map((id) => {
    if (!visibleIdSet.has(id)) {
      return id;
    }

    const nextVisibleId = reorderedVisibleIds[reorderedVisibleIndex];
    reorderedVisibleIndex += 1;
    return nextVisibleId;
  });
};

export const matchesScheduleFilters = (schedule: ScheduleListItem, filters: ActiveFilters) => {
  const statusFilters = filters.dropdownFilters.status;
  if (statusFilters && statusFilters.length > 0 && !statusFilters.includes(schedule.status)) {
    return false;
  }

  const actionFilters = filters.dropdownFilters.action;
  if (actionFilters && actionFilters.length > 0 && !actionFilters.includes(schedule.action)) {
    return false;
  }

  return true;
};

export const hasActiveScheduleFilters = (filters: ActiveFilters) =>
  Object.values(filters.dropdownFilters).some((values) => values.length > 0);

export const formatTimezoneLabel = (timeZone: string, date = new Date()) => {
  const resolvedTimeZone = timeZone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

  try {
    const shortName =
      Intl.DateTimeFormat(undefined, { timeZone: resolvedTimeZone, timeZoneName: "short" })
        .formatToParts(date)
        .find((part) => part.type === "timeZoneName")?.value ?? resolvedTimeZone;

    return `All times ${resolvedTimeZone} (${shortName})`;
  } catch {
    return `All times ${resolvedTimeZone}`;
  }
};

export const formatClientTimezoneLabel = (date = new Date()) => {
  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const shortName =
    Intl.DateTimeFormat(undefined, { timeZone, timeZoneName: "short" })
      .formatToParts(date)
      .find((part) => part.type === "timeZoneName")?.value ?? timeZone;

  const offsetMinutes = -date.getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? "+" : "-";
  const absoluteMinutes = Math.abs(offsetMinutes);
  const hours = Math.floor(absoluteMinutes / 60);
  const minutes = absoluteMinutes % 60;
  const offsetLabel = minutes === 0 ? `${hours}` : `${hours}:${String(minutes).padStart(2, "0")}`;

  return `All times UTC${sign}${offsetLabel} (${shortName})`;
};
