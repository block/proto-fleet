import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import useScheduleApi, { type ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import {
  activeScheduleCols,
  formatClientTimezoneLabel,
  getDefaultScheduleSortDirection,
  hasActiveScheduleFilters,
  matchesScheduleFilters,
  reorderScheduleIdsByDrop,
  SCHEDULE_EMPTY_STATE_DESCRIPTION,
  SCHEDULE_PAGE_DESCRIPTION,
  scheduleColTitles,
  scheduleColumnAriaLabels,
  scheduleFilters,
  SORTABLE_COLUMNS,
  sortSchedules,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import type { ScheduleColumn } from "@/protoFleet/features/settings/components/Schedules/constants";
import createScheduleColConfig from "@/protoFleet/features/settings/components/Schedules/scheduleColConfig";
import { Edit, Pause, Play, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";
import type { ListAction, SortDirection } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const defaultActiveFilters: ActiveFilters = {
  buttonFilters: ["all"],
  dropdownFilters: {},
};

const getErrorMessage = (error: unknown, fallbackMessage: string) =>
  error instanceof Error && error.message ? error.message : fallbackMessage;

const SchedulesPage = () => {
  const { schedules, isLoading, refreshSchedules, pauseSchedule, resumeSchedule, deleteSchedule, reorderSchedules } =
    useScheduleApi();
  const { isPhone, isTablet } = useWindowDimensions();
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>(defaultActiveFilters);
  const [currentSort, setCurrentSort] = useState<{ field: ScheduleColumn; direction: SortDirection }>();
  const [hasCompletedInitialLoad, setHasCompletedInitialLoad] = useState(false);

  useEffect(() => {
    let isSubscribed = true;

    void refreshSchedules()
      .catch((error) => {
        pushToast({
          message: getErrorMessage(error, "Failed to load schedules"),
          status: STATUSES.error,
        });
      })
      .finally(() => {
        if (isSubscribed) {
          setHasCompletedInitialLoad(true);
        }
      });

    return () => {
      isSubscribed = false;
    };
  }, [refreshSchedules]);

  const timezoneLabel = useMemo(() => formatClientTimezoneLabel(), []);
  const colConfig = useMemo(() => createScheduleColConfig(), []);
  const sortedSchedules = useMemo(() => sortSchedules(schedules, currentSort), [schedules, currentSort]);
  const filtersActive = useMemo(() => hasActiveScheduleFilters(activeFilters), [activeFilters]);

  const handleSort = useCallback((field: ScheduleColumn, direction: SortDirection) => {
    setCurrentSort({ field, direction });
  }, []);

  const handleRowReorder = useCallback(
    async (activeId: string, overId: string, visibleItemKeys: string[]) => {
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

      try {
        await reorderSchedules(reorderedScheduleIds);
        if (currentSort) {
          setCurrentSort(undefined);
        }
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to reorder schedules"),
          status: STATUSES.error,
        });
      }
    },
    [currentSort, reorderSchedules, schedules],
  );

  const handlePauseResume = useCallback(
    async (schedule: ScheduleListItem) => {
      try {
        if (schedule.status === "paused") {
          await resumeSchedule(schedule.id);
          return;
        }

        if (schedule.status !== "completed") {
          await pauseSchedule(schedule.id);
        }
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update schedule"),
          status: STATUSES.error,
        });
      }
    },
    [pauseSchedule, resumeSchedule],
  );

  const handleDelete = useCallback(
    async (schedule: ScheduleListItem) => {
      try {
        await deleteSchedule(schedule.id);
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to delete schedule"),
          status: STATUSES.error,
        });
      }
    },
    [deleteSchedule],
  );

  const handleEdit = useCallback((_schedule: ScheduleListItem) => undefined, []);

  const rowActions = useMemo<ListAction<ScheduleListItem>[]>(
    () => [
      {
        title: "Edit",
        icon: <Edit />,
        actionHandler: handleEdit,
        disabled: true,
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

  if (isLoading || !hasCompletedInitialLoad) {
    return (
      <div className="flex justify-center py-20">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (schedules.length === 0) {
    return (
      <div
        className={clsx("flex items-center rounded-xl bg-landing-page p-6 sm:p-20", {
          "h-full": !isPhone && !isTablet,
          "flex-1": isPhone || isTablet,
        })}
      >
        <div className="flex flex-col gap-6">
          <Header
            title="Schedules"
            subtitle={SCHEDULE_EMPTY_STATE_DESCRIPTION}
            titleSize="text-heading-400"
            subtitleSize="text-400"
            subtitleClassName="mt-1"
            className="items-center"
          />
          <Button variant={variants.primary} className="w-fit" text="Add a schedule" disabled />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header
          title="Schedules"
          titleSize="text-heading-300"
          description={SCHEDULE_PAGE_DESCRIPTION}
          descriptionClassName="max-w-none"
        />
        <Button
          variant={variants.primary}
          size={sizes.base}
          text="Add a schedule"
          disabled
          className="shrink-0 phone:w-full"
        />
      </div>

      <List<ScheduleListItem, string, ScheduleColumn>
        items={sortedSchedules}
        itemKey="id"
        activeCols={activeScheduleCols}
        colTitles={scheduleColTitles}
        columnHeaderAriaLabels={scheduleColumnAriaLabels}
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
        tableClassName="mb-2 w-max !table-auto"
      />
      <div className="px-2 pb-2 text-200 text-text-primary-70">{timezoneLabel}</div>
    </div>
  );
};

export default SchedulesPage;
