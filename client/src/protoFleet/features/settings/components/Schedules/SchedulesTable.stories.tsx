import { useCallback, useMemo, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import type { Schedule } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import {
  activeScheduleCols,
  formatClientTimezoneLabel,
  getDefaultScheduleSortDirection,
  hasActiveScheduleFilters,
  matchesScheduleFilters,
  reorderScheduleIdsByDrop,
  scheduleColTitles,
  scheduleFilters,
  SORTABLE_COLUMNS,
  sortSchedules,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import type { ScheduleColumn } from "@/protoFleet/features/settings/components/Schedules/constants";
import createScheduleColConfig from "@/protoFleet/features/settings/components/Schedules/scheduleColConfig";
import { Edit, Pause, Play, Trash } from "@/shared/assets/icons";
import List from "@/shared/components/List";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";
import type { ListAction, SortDirection } from "@/shared/components/List/types";

const defaultActiveFilters: ActiveFilters = {
  buttonFilters: ["all"],
  dropdownFilters: {},
};

const createDemoSchedule = (schedule: Omit<ScheduleListItem, "rawSchedule">): ScheduleListItem => ({
  ...schedule,
  rawSchedule: {} as Schedule,
});

const demoSchedules: ScheduleListItem[] = [
  createDemoSchedule({
    id: "weekday-ramp-up",
    priority: 1,
    name: "Weekday ramp-up",
    targetSummary: "Applies to all miners",
    scheduleSummary: "Weekdays · 6:00 AM – 10:00 PM",
    nextRunSummary: "Runs tomorrow at 6:00 AM",
    action: "setPowerTarget",
    status: "running",
    createdBy: "admin@fleet.io",
  }),
  createDemoSchedule({
    id: "night-shift",
    priority: 2,
    name: "Night shift",
    targetSummary: "Applies to all miners",
    scheduleSummary: "Every day · 10:00 PM",
    nextRunSummary: "Runs today at 10:00 PM",
    action: "sleep",
    status: "paused",
    createdBy: "admin@fleet.io",
  }),
  createDemoSchedule({
    id: "weekend-saver",
    priority: 3,
    name: "Weekend saver",
    targetSummary: "Applies to 24 miners",
    scheduleSummary: "Weekends · 8:00 AM – 6:00 PM",
    nextRunSummary: "Runs Sat at 8:00 AM",
    action: "setPowerTarget",
    status: "active",
    createdBy: "jmarr@fleet.io",
  }),
  createDemoSchedule({
    id: "monthly-reboot",
    priority: 4,
    name: "Monthly reboot",
    targetSummary: "Applies to all miners",
    scheduleSummary: "1st day of month · 2:00 AM",
    nextRunSummary: "Runs on Apr 1, 2026, 2:00 AM",
    action: "reboot",
    status: "active",
    createdBy: "admin@fleet.io",
  }),
  createDemoSchedule({
    id: "deep-sleep-window",
    priority: 5,
    name: "Deep sleep window",
    targetSummary: "Applies to 12 miners",
    scheduleSummary: "Weeknights · 1:00 AM",
    nextRunSummary: null,
    action: "sleep",
    status: "completed",
    createdBy: "ops@fleet.io",
  }),
];

type SchedulesTableStoryProps = {
  initialSchedules?: ScheduleListItem[];
};

const SchedulesTableStory = ({ initialSchedules = demoSchedules }: SchedulesTableStoryProps) => {
  const [schedules, setSchedules] = useState<ScheduleListItem[]>(initialSchedules);
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>(defaultActiveFilters);
  const [currentSort, setCurrentSort] = useState<{ field: ScheduleColumn; direction: SortDirection }>();
  const colConfig = useMemo(() => createScheduleColConfig(), []);
  const timezoneLabel = useMemo(() => formatClientTimezoneLabel(), []);
  const sortedSchedules = useMemo(() => sortSchedules(schedules, currentSort), [currentSort, schedules]);
  const filtersActive = useMemo(() => hasActiveScheduleFilters(activeFilters), [activeFilters]);

  const handleSort = useCallback((field: ScheduleColumn, direction: SortDirection) => {
    setCurrentSort({ field, direction });
  }, []);

  const handlePauseResume = useCallback((schedule: ScheduleListItem) => {
    action(schedule.status === "paused" ? "Resume schedule" : "Pause schedule")(schedule.id);

    if (schedule.status === "completed") {
      return;
    }

    setSchedules((current) =>
      current.map((item) =>
        item.id === schedule.id
          ? {
              ...item,
              status: item.status === "paused" ? "active" : "paused",
            }
          : item,
      ),
    );
  }, []);

  const handleDelete = useCallback((schedule: ScheduleListItem) => {
    action("Delete schedule")(schedule.id);
    setSchedules((current) => current.filter((item) => item.id !== schedule.id));
  }, []);

  const handleEdit = useCallback((schedule: ScheduleListItem) => {
    action("Edit schedule")(schedule.id);
  }, []);

  const handleRowReorder = useCallback(
    (activeId: string, overId: string, visibleItemKeys: string[]) => {
      const priorityOrderedIds = sortSchedules(schedules).map((schedule) => schedule.id);
      const reorderedScheduleIds = reorderScheduleIdsByDrop({
        activeId,
        overId,
        visibleItemKeys,
        priorityOrderedIds,
      });

      if (!reorderedScheduleIds) {
        return;
      }

      action("Reorder schedules")(reorderedScheduleIds);
      setSchedules((current) => {
        const scheduleById = new Map(current.map((schedule) => [schedule.id, schedule]));

        return reorderedScheduleIds.flatMap((id, index) => {
          const schedule = scheduleById.get(id);

          if (!schedule) {
            return [];
          }

          return [{ ...schedule, priority: index + 1 }];
        });
      });

      if (currentSort) {
        setCurrentSort(undefined);
      }
    },
    [currentSort, schedules],
  );

  const rowActions = useMemo<ListAction<ScheduleListItem>[]>(
    () => [
      {
        title: "Edit",
        icon: <Edit />,
        actionHandler: handleEdit,
        showDividerAfter: false,
      },
      {
        title: (schedule) => (schedule.status === "paused" ? "Resume" : "Pause"),
        icon: (schedule) => (schedule.status === "paused" ? <Play /> : <Pause />),
        actionHandler: handlePauseResume,
        hidden: (schedule) => schedule.status === "completed",
      },
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive",
        actionHandler: handleDelete,
      },
    ],
    [handleDelete, handleEdit, handlePauseResume],
  );

  const emptyStateRow = (
    <div className="flex flex-col items-center justify-center gap-1 py-12 text-center">
      <p className="text-heading-200 text-text-primary">No schedules match those filters</p>
      <p className="text-300 text-text-primary-70">
        Try clearing one or more filters to see the rest of your schedules.
      </p>
    </div>
  );

  return (
    <div className="min-h-screen w-screen bg-white p-6">
      <div className="w-full">
        <List<ScheduleListItem, string, ScheduleColumn>
          items={sortedSchedules}
          itemKey="id"
          activeCols={activeScheduleCols}
          colTitles={scheduleColTitles}
          colConfig={colConfig}
          total={schedules.length}
          hideTotal
          itemName={{ singular: "schedule", plural: "schedules" }}
          filters={scheduleFilters}
          filterItem={matchesScheduleFilters}
          onFilterChange={setActiveFilters}
          emptyStateRow={filtersActive ? emptyStateRow : undefined}
          sortableColumns={SORTABLE_COLUMNS}
          currentSort={currentSort}
          onSort={handleSort}
          onRowReorder={handleRowReorder}
          rowDragHandleColumn="priority"
          stickyFirstColumn={false}
          getDefaultSortDirection={getDefaultScheduleSortDirection}
          actions={rowActions}
          applyColumnWidthsToCells
          tableClassName="mb-2 w-full phone:w-max phone:!table-auto"
        />
        <div className="px-2 pb-2 text-200 text-text-primary-70">{timezoneLabel}</div>
      </div>
    </div>
  );
};

const meta = {
  title: "Proto Fleet/Settings/SchedulesTable",
  component: SchedulesTableStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof SchedulesTableStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};
