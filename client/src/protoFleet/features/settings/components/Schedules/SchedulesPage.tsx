import { useCallback, useEffect, useMemo, useState } from "react";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useScheduleApiContext } from "@/protoFleet/api/ScheduleApiContext";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
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
  scheduleTableClassName,
  SORTABLE_COLUMNS,
  sortSchedules,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import type { ScheduleColumn } from "@/protoFleet/features/settings/components/Schedules/constants";
import createScheduleColConfig from "@/protoFleet/features/settings/components/Schedules/scheduleColConfig";
import ScheduleModal from "@/protoFleet/features/settings/components/Schedules/ScheduleModal";
import { Edit, Pause, Play, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";
import type { ListAction, SortDirection } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const defaultActiveFilters: ActiveFilters = {
  buttonFilters: ["all"],
  dropdownFilters: {},
};

const SchedulesPage = () => {
  const {
    schedules,
    isLoading,
    refreshSchedules,
    createSchedule,
    updateSchedule,
    pauseSchedule,
    resumeSchedule,
    deleteSchedule,
    reorderSchedules,
  } = useScheduleApiContext();
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>(defaultActiveFilters);
  const [currentSort, setCurrentSort] = useState<{ field: ScheduleColumn; direction: SortDirection }>();
  const [hasCompletedInitialLoad, setHasCompletedInitialLoad] = useState(false);
  const [activeModalState, setActiveModalState] = useState<{ mode: "create" } | { mode: "edit"; scheduleId: string }>();

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

  const handleEdit = useCallback((schedule: ScheduleListItem) => {
    setActiveModalState({ mode: "edit", scheduleId: schedule.id });
  }, []);

  const handleOpenCreateModal = useCallback(() => {
    setActiveModalState({ mode: "create" });
  }, []);

  const handleDismissModal = useCallback(() => {
    setActiveModalState(undefined);
  }, []);

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

  const activeSchedule =
    activeModalState?.mode === "edit"
      ? schedules.find((schedule) => schedule.id === activeModalState.scheduleId)
      : undefined;
  const isScheduleModalOpen =
    activeModalState !== undefined && (activeModalState.mode === "create" || activeSchedule !== undefined);
  const scheduleModal = isScheduleModalOpen ? (
    <ScheduleModal
      open
      schedule={activeSchedule}
      onDismiss={handleDismissModal}
      onCreateSchedule={createSchedule}
      onUpdateSchedule={updateSchedule}
      onDeleteSchedule={deleteSchedule}
      onPauseSchedule={pauseSchedule}
      onResumeSchedule={resumeSchedule}
    />
  ) : null;

  const emptyStateRow = filtersActive ? (
    <div className="flex flex-col items-center justify-center gap-1 py-12 text-center">
      <p className="text-heading-200 text-text-primary">No schedules match those filters</p>
      <p className="text-300 text-text-primary-70">
        Try clearing one or more filters to see the rest of your schedules.
      </p>
    </div>
  ) : (
    <div className="py-10 text-center text-text-primary-50">No schedules yet. {SCHEDULE_EMPTY_STATE_DESCRIPTION}</div>
  );

  if (isLoading || !hasCompletedInitialLoad) {
    return (
      <div className="flex justify-center py-20">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header title="Schedules" titleSize="text-heading-300" description={SCHEDULE_PAGE_DESCRIPTION} />
        <Button
          variant={variants.primary}
          size={sizes.base}
          text="Add a schedule"
          onClick={handleOpenCreateModal}
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
        emptyStateRow={emptyStateRow}
        sortableColumns={SORTABLE_COLUMNS}
        currentSort={currentSort}
        onSort={handleSort}
        onRowReorder={handleRowReorder}
        rowDragHandleColumn="priority"
        stickyFirstColumn={false}
        getDefaultSortDirection={getDefaultScheduleSortDirection}
        actions={rowActions}
        applyColumnWidthsToCells
        tableClassName={scheduleTableClassName}
      />
      <div className="px-2 pb-2 text-200 text-text-primary-70">{timezoneLabel}</div>

      {scheduleModal}
    </div>
  );
};

export default SchedulesPage;
