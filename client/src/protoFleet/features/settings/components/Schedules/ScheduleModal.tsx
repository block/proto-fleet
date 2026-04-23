import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  type CreateScheduleRequest,
  DayOfWeek,
  type UpdateScheduleRequest,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import useFleet from "@/protoFleet/api/useFleet";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import {
  formatClientTimezoneLabel,
  formatTimezoneLabel,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import GroupSelectionModal from "@/protoFleet/features/settings/components/Schedules/GroupSelectionModal";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import RackSelectionModal from "@/protoFleet/features/settings/components/Schedules/RackSelectionModal";
import SchedulePreview from "@/protoFleet/features/settings/components/Schedules/SchedulePreview";
import {
  actionOptions,
  buildScheduleRequest,
  createDefaultScheduleFormValues,
  createScheduleFormValuesFromSchedule,
  endBehaviorOptions,
  frequencyOptions,
  getNextEndTimeAfterStart,
  powerTargetModeOptions,
  type ScheduleFormErrors,
  type ScheduleFormValues,
  scheduleTypeOptions,
  timeOptions,
  validateSchedule,
} from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";
import {
  formatDateValue,
  parseDate as parseScheduleDate,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";
import { ChevronDown } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import Select from "@/shared/components/Select";
import { type Position, positions } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type TouchedFields = Partial<Record<keyof ScheduleFormErrors, true>>;

const validationFieldKeys: Array<keyof ScheduleFormErrors> = [
  "name",
  "startDate",
  "startTime",
  "endTime",
  "daysOfWeek",
  "dayOfMonth",
  "endDate",
];

interface ScheduleModalProps {
  open: boolean;
  schedule?: ScheduleListItem;
  onDismiss: () => void;
  onCreateSchedule: (request: CreateScheduleRequest) => Promise<unknown>;
  onUpdateSchedule: (request: UpdateScheduleRequest) => Promise<unknown>;
  onDeleteSchedule: (scheduleId: string) => Promise<void>;
  onPauseSchedule: (scheduleId: string) => Promise<void>;
  onResumeSchedule: (scheduleId: string) => Promise<void>;
}

const sectionTitleClassName = "text-emphasis-300 text-text-primary";
const sectionBodyClassName = "grid gap-4";
const popoverViewportPadding = minimalMargin * 2;

const getTargetButtonLabel = (count: number, singular: string) =>
  count > 0 ? `${count} ${count === 1 ? singular : `${singular}s`}` : "Select";

interface TargetSelectButtonProps {
  label: string;
  value: string;
  onClick: () => void;
}

const TargetSelectButton = ({ label, value, onClick }: TargetSelectButtonProps) => {
  const isPlaceholder = value === "Select";

  return (
    <button
      type="button"
      onClick={onClick}
      className="relative flex h-14 w-full items-center justify-between rounded-lg border border-border-5 bg-surface-base pr-4 pl-4 text-left outline-hidden"
    >
      <div className="flex min-w-0 flex-col pt-[18px]">
        <span className="absolute top-[7px] text-200 text-text-primary-50">{label}</span>
        <div
          className={isPlaceholder ? "truncate text-300 text-text-primary-50" : "truncate text-300 text-text-primary"}
        >
          {value}
        </div>
      </div>
      <ChevronDown width="w-3" className="shrink-0 text-text-primary-70" />
    </button>
  );
};

const weekdayMenuOptions: Array<{ value: DayOfWeek; label: string; shortLabel: string }> = [
  { value: DayOfWeek.SUNDAY, label: "Sunday", shortLabel: "Sun" },
  { value: DayOfWeek.MONDAY, label: "Monday", shortLabel: "Mon" },
  { value: DayOfWeek.TUESDAY, label: "Tuesday", shortLabel: "Tue" },
  { value: DayOfWeek.WEDNESDAY, label: "Wednesday", shortLabel: "Wed" },
  { value: DayOfWeek.THURSDAY, label: "Thursday", shortLabel: "Thu" },
  { value: DayOfWeek.FRIDAY, label: "Friday", shortLabel: "Fri" },
  { value: DayOfWeek.SATURDAY, label: "Saturday", shortLabel: "Sat" },
];

const formatSelectedDays = (selectedDays: DayOfWeek[]) => {
  if (selectedDays.length === 0) {
    return "Select days";
  }

  if (selectedDays.length === weekdayMenuOptions.length) {
    return "Every day";
  }

  const labelByDay = new Map(weekdayMenuOptions.map((option) => [option.value, option.shortLabel]));

  return [...selectedDays]
    .sort((left, right) => left - right)
    .map((day) => labelByDay.get(day))
    .filter(Boolean)
    .join(", ");
};

interface WeekdaySelectProps {
  id: string;
  value: DayOfWeek[];
  onChange: (value: DayOfWeek[]) => void;
  error?: boolean | string;
}

const WeekdaySelectContent = ({ id, value, onChange, error }: WeekdaySelectProps) => {
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const listboxRef = useRef<HTMLDivElement>(null);
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>();
  const [popoverMaxHeight, setPopoverMaxHeight] = useState<number | undefined>();
  const selectedLabel = formatSelectedDays(value);
  const hasValue = value.length > 0;

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  useEffect(() => {
    if (!open || !triggerRef.current) {
      return;
    }

    const updatePopoverLayout = () => {
      if (!triggerRef.current) {
        return;
      }

      const triggerRect = triggerRef.current.getBoundingClientRect();
      const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
      const spaceAbove = triggerRect.top - popoverViewportPadding;
      const spaceBelow = viewportHeight - triggerRect.bottom - popoverViewportPadding;
      const shouldOpenAbove = spaceAbove > spaceBelow;

      setTriggerWidth(triggerRect.width);
      setPopoverPosition(shouldOpenAbove ? positions["top right"] : positions["bottom right"]);
      setPopoverMaxHeight(Math.max(Math.floor(shouldOpenAbove ? spaceAbove : spaceBelow), 0));
    };

    updatePopoverLayout();

    window.addEventListener("resize", updatePopoverLayout);
    window.visualViewport?.addEventListener("resize", updatePopoverLayout);

    return () => {
      window.removeEventListener("resize", updatePopoverLayout);
      window.visualViewport?.removeEventListener("resize", updatePopoverLayout);
    };
  }, [open, triggerRef]);

  useEffect(() => {
    if (!open || !listboxRef.current) {
      return;
    }

    listboxRef.current.scrollTop = 0;
  }, [open]);

  const toggleDay = (day: DayOfWeek) => {
    onChange(
      value.includes(day)
        ? value.filter((currentDay) => currentDay !== day)
        : [...value, day].sort((left, right) => left - right),
    );
  };

  return (
    <div className="relative">
      <div ref={triggerRef}>
        <button
          id={id}
          type="button"
          aria-label="Days"
          aria-haspopup="listbox"
          aria-expanded={open}
          onClick={() => setOpen((prev) => !prev)}
          className={clsx(
            "peer flex h-14 w-full items-center justify-between rounded-lg pr-4 pl-4 text-left outline-hidden",
            "transition duration-200 ease-in-out",
            "bg-surface-base",
            { "border border-intent-critical-50": error && !open },
            { "border border-border-5": !open && !error },
            { "border border-border-20 ring-4 ring-core-primary-5": open && !error },
            { "border border-intent-critical-50 ring-4 ring-intent-critical-20": open && error },
          )}
        >
          <div className="flex min-w-0 flex-col pt-[18px]">
            <span className="absolute top-[7px] text-200 text-text-primary-50">Days</span>
            <span className={clsx("truncate text-300", hasValue ? "text-text-primary" : "text-text-primary-50")}>
              {selectedLabel}
            </span>
          </div>
          <ChevronDown
            width="w-3"
            className={clsx("shrink-0 text-text-primary-70 transition-transform", { "rotate-180": open })}
          />
        </button>
      </div>

      {open ? (
        <Popover
          position={popoverPosition}
          className="!w-auto !space-y-0 !rounded-xl border border-border-5 !bg-surface-elevated-base !p-0 !shadow-300 !backdrop-blur-none"
          closePopover={() => setOpen(false)}
          closeIgnoreSelectors={[`#${id}`]}
        >
          <div
            ref={listboxRef}
            className="max-h-[calc(100vh-2rem)] overflow-y-auto overscroll-contain p-1.5"
            role="listbox"
            aria-label="Days options"
            style={{
              minWidth: triggerWidth,
              maxHeight: popoverMaxHeight,
            }}
          >
            {weekdayMenuOptions.map((option) => {
              const checked = value.includes(option.value);

              return (
                <div
                  key={option.value}
                  role="option"
                  aria-selected={checked}
                  className="flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
                  onClick={() => toggleDay(option.value)}
                >
                  <Checkbox checked={checked} />
                  <span className="text-emphasis-300">{option.label}</span>
                </div>
              );
            })}
          </div>
        </Popover>
      ) : null}

      <div
        className={clsx(
          "text-200 text-intent-critical-fill",
          "transition-[opacity,max-height,margin-top] duration-200 ease-in-out",
          { "max-h-0 opacity-0": !error || error === true },
          { "mt-2 max-h-10 opacity-100": error && error !== true },
        )}
      >
        <div className="flex items-center space-x-1">
          <div className="h-1 w-2.5 rounded-full bg-intent-critical-20" />
          <div>{error !== true ? error : null}</div>
        </div>
      </div>
    </div>
  );
};

const WeekdaySelect = (props: WeekdaySelectProps) => (
  <PopoverProvider>
    <WeekdaySelectContent {...props} />
  </PopoverProvider>
);

const ScheduleModal = ({
  open,
  schedule,
  onDismiss,
  onCreateSchedule,
  onUpdateSchedule,
  onDeleteSchedule,
  onPauseSchedule,
  onResumeSchedule,
}: ScheduleModalProps) => {
  const isEditMode = Boolean(schedule);
  const { listRacks, listGroups } = useDeviceSets();
  const { totalMiners: totalAvailableMiners, hasInitialLoadCompleted: hasLoadedAvailableMiners } = useFleet({
    pageSize: 1,
    pairingStatuses: [PairingStatus.PAIRED],
  });
  const [values, setValues] = useState<ScheduleFormValues>(() => createDefaultScheduleFormValues());
  const [availableRackIds, setAvailableRackIds] = useState<Set<string>>(new Set());
  const [availableGroupIds, setAvailableGroupIds] = useState<Set<string>>(new Set());
  const [hasLoadedAvailableGroups, setHasLoadedAvailableGroups] = useState(false);
  const [errors, setErrors] = useState<ScheduleFormErrors>({});
  const [touchedFields, setTouchedFields] = useState<TouchedFields>({});
  const [showAllErrors, setShowAllErrors] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isUpdatingStatus, setIsUpdatingStatus] = useState(false);
  const [showRackSelectionModal, setShowRackSelectionModal] = useState(false);
  const [showGroupSelectionModal, setShowGroupSelectionModal] = useState(false);
  const [showMinerSelectionModal, setShowMinerSelectionModal] = useState(false);
  const initializedFormSourceRef = useRef<string | null>(null);
  const valuesRef = useRef(values);
  const formSourceKey = schedule?.id ?? "__create__";

  useEffect(() => {
    if (!open) {
      initializedFormSourceRef.current = null;
      return;
    }

    if (initializedFormSourceRef.current === formSourceKey) {
      return;
    }

    initializedFormSourceRef.current = formSourceKey;
    setValues(
      schedule ? createScheduleFormValuesFromSchedule(schedule.rawSchedule) : createDefaultScheduleFormValues(),
    );
    setErrors({});
    setTouchedFields({});
    setShowAllErrors(false);
    setShowRackSelectionModal(false);
    setShowGroupSelectionModal(false);
    setShowMinerSelectionModal(false);
    setIsSaving(false);
    setIsDeleting(false);
    setIsUpdatingStatus(false);
  }, [formSourceKey, open, schedule]);

  useEffect(() => {
    if (!open) {
      return;
    }

    setAvailableGroupIds(new Set());
    setHasLoadedAvailableGroups(false);

    listRacks({
      onSuccess: (deviceSets) => {
        const validIds = new Set(deviceSets.map((rack) => rack.id.toString()));
        setAvailableRackIds(validIds);
        setValues((current) => {
          const pruned = current.rackTargetIds.filter((id) => validIds.has(id));
          return pruned.length === current.rackTargetIds.length ? current : { ...current, rackTargetIds: pruned };
        });
      },
    });
    listGroups({
      onSuccess: (deviceSets) => {
        const validIds = new Set(deviceSets.map((group) => group.id.toString()));
        setAvailableGroupIds(validIds);
        setHasLoadedAvailableGroups(true);
        setValues((current) => {
          const pruned = current.groupTargetIds.filter((id) => validIds.has(id));
          return pruned.length === current.groupTargetIds.length ? current : { ...current, groupTargetIds: pruned };
        });
      },
    });
  }, [listGroups, listRacks, open]);

  useEffect(() => {
    valuesRef.current = values;
  }, [values]);

  const setNextValues = useCallback(
    (updater: ScheduleFormValues | ((current: ScheduleFormValues) => ScheduleFormValues)) => {
      setValues((current) => {
        const next = typeof updater === "function" ? updater(current) : updater;
        valuesRef.current = next;

        if (showAllErrors || Object.keys(touchedFields).length > 0) {
          setErrors(validateSchedule(next));
        }

        return next;
      });
    },
    [showAllErrors, touchedFields],
  );

  const handleBlur = useCallback(
    (field: keyof ScheduleFormErrors) => {
      setTouchedFields((current) => ({ ...current, [field]: true }));
      setErrors(validateSchedule(values));
    },
    [values],
  );

  const getVisibleError = useCallback(
    (field: keyof ScheduleFormErrors) => (showAllErrors || touchedFields[field] ? errors[field] : undefined),
    [errors, showAllErrors, touchedFields],
  );

  const updateDateValidation = useCallback((field: "startDate" | "endDate", nextValues: ScheduleFormValues) => {
    setTouchedFields((touched) => ({ ...touched, [field]: true }));
    setErrors(validateSchedule(nextValues));
  }, []);

  const touchDateField = useCallback(
    (field: "startDate" | "endDate") => {
      updateDateValidation(field, valuesRef.current);
    },
    [updateDateValidation],
  );

  const updateDateField = useCallback(
    (field: "startDate" | "endDate", date: Date) => {
      const nextValue = formatDateValue(date);
      const nextValues = { ...valuesRef.current, [field]: nextValue };
      valuesRef.current = nextValues;
      setValues(nextValues);
      updateDateValidation(field, nextValues);
    },
    [updateDateValidation],
  );

  const selectedStartDate = values.startDate ? (parseScheduleDate(values.startDate) ?? undefined) : undefined;
  const selectedEndDate = values.endDate ? (parseScheduleDate(values.endDate) ?? undefined) : undefined;
  const isEndDateDisabled = useCallback(
    (date: Date) => (selectedStartDate ? date.getTime() < selectedStartDate.getTime() : false),
    [selectedStartDate],
  );

  const handleActionChange = useCallback(
    (action: string) => {
      setNextValues((current) => {
        const nextAction = action as ScheduleFormValues["action"];
        const nextValues: ScheduleFormValues = {
          ...current,
          action: nextAction,
        };

        if (nextAction !== "setPowerTarget") {
          nextValues.endTime = "";
        } else if (current.scheduleType === "recurring" && !current.endTime) {
          nextValues.endTime = getNextEndTimeAfterStart(current.startTime);
        }

        return nextValues;
      });
    },
    [setNextValues],
  );

  const handleScheduleTypeChange = useCallback(
    (scheduleType: string) => {
      setNextValues((current) => {
        const nextType = scheduleType as ScheduleFormValues["scheduleType"];
        const nextValues = {
          ...current,
          scheduleType: nextType,
        };

        if (nextType === "recurring" && current.action === "setPowerTarget" && !current.endTime) {
          nextValues.endTime = getNextEndTimeAfterStart(current.startTime);
        }

        if (nextType === "oneTime") {
          nextValues.endTime = "";
        }

        return nextValues;
      });
    },
    [setNextValues],
  );

  const handleSave = useCallback(async () => {
    const nextErrors = validateSchedule(values);
    setErrors(nextErrors);
    setShowAllErrors(true);
    setTouchedFields(
      validationFieldKeys.reduce<TouchedFields>((result, field) => {
        result[field] = true;
        return result;
      }, {}),
    );

    if (Object.keys(nextErrors).length > 0) {
      return;
    }

    setIsSaving(true);

    try {
      const request = buildScheduleRequest(values, schedule?.id);

      if (schedule) {
        await onUpdateSchedule(request as UpdateScheduleRequest);
        pushToast({
          message: "Schedule updated",
          status: STATUSES.success,
        });
      } else {
        await onCreateSchedule(request as CreateScheduleRequest);
        pushToast({
          message: "Schedule created",
          status: STATUSES.success,
        });
      }

      onDismiss();
    } catch (error) {
      pushToast({
        message: error instanceof Error && error.message ? error.message : "Failed to save schedule",
        status: STATUSES.error,
      });
    } finally {
      setIsSaving(false);
    }
  }, [onCreateSchedule, onDismiss, onUpdateSchedule, schedule, values]);

  const handleDelete = useCallback(async () => {
    if (!schedule) {
      return;
    }

    setIsDeleting(true);

    try {
      await onDeleteSchedule(schedule.id);
      pushToast({
        message: "Schedule deleted",
        status: STATUSES.success,
      });
      onDismiss();
    } catch (error) {
      pushToast({
        message: error instanceof Error && error.message ? error.message : "Failed to delete schedule",
        status: STATUSES.error,
      });
    } finally {
      setIsDeleting(false);
    }
  }, [onDeleteSchedule, onDismiss, schedule]);

  const handlePauseResume = useCallback(async () => {
    if (!schedule || schedule.status === "completed") {
      return;
    }

    setIsUpdatingStatus(true);

    try {
      if (schedule.status === "paused") {
        await onResumeSchedule(schedule.id);
        pushToast({
          message: "Schedule resumed",
          status: STATUSES.success,
        });
      } else {
        await onPauseSchedule(schedule.id);
        pushToast({
          message: "Schedule paused",
          status: STATUSES.success,
        });
      }
    } catch (error) {
      pushToast({
        message: error instanceof Error && error.message ? error.message : "Failed to update schedule status",
        status: STATUSES.error,
      });
    } finally {
      setIsUpdatingStatus(false);
    }
  }, [onPauseSchedule, onResumeSchedule, schedule]);

  const isBusy = isSaving || isDeleting || isUpdatingStatus;
  const isRunningSchedule = schedule?.status === "running";
  const canSave = useMemo(
    () => !isBusy && !isRunningSchedule && Object.keys(validateSchedule(values)).length === 0,
    [isBusy, isRunningSchedule, values],
  );
  const showRecurringFields = values.scheduleType === "recurring";
  const showPowerTargetFields = values.action === "setPowerTarget";
  const showPowerTargetWindow = values.scheduleType === "recurring" && values.action === "setPowerTarget";
  const validRackTargetCount = useMemo(
    () => values.rackTargetIds.filter((rackId) => availableRackIds.has(rackId)).length,
    [availableRackIds, values.rackTargetIds],
  );
  const validGroupTargetCount = useMemo(() => {
    if (!hasLoadedAvailableGroups) {
      return values.groupTargetIds.length;
    }
    return values.groupTargetIds.filter((groupId) => availableGroupIds.has(groupId)).length;
  }, [availableGroupIds, hasLoadedAvailableGroups, values.groupTargetIds]);
  const validMinerTargetCount = useMemo(() => {
    if (hasLoadedAvailableMiners && totalAvailableMiners === 0) {
      return 0;
    }

    return values.minerTargetIds.length;
  }, [hasLoadedAvailableMiners, totalAvailableMiners, values.minerTargetIds.length]);

  if (!open) {
    return null;
  }

  const hasSubModalOpen = showRackSelectionModal || showGroupSelectionModal || showMinerSelectionModal;

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title={isEditMode ? "Edit schedule" : "Add a schedule"}
        onDismiss={hasSubModalOpen ? undefined : onDismiss}
        isBusy={isBusy}
        closeAriaLabel="Close schedule editor"
        buttons={[
          ...(isEditMode
            ? [
                {
                  text: "Delete",
                  variant: variants.secondaryDanger,
                  onClick: () => void handleDelete(),
                  disabled: isBusy,
                },
                ...(schedule?.status !== "completed"
                  ? [
                      {
                        text: schedule?.status === "paused" ? "Resume" : "Pause",
                        variant: variants.secondary,
                        onClick: () => void handlePauseResume(),
                        disabled: isBusy,
                      },
                    ]
                  : []),
              ]
            : []),
          {
            text: "Save",
            variant: variants.primary,
            onClick: () => void handleSave(),
            disabled: !canSave,
            loading: isSaving,
          },
        ]}
        primaryPane={
          <section className="flex flex-col gap-10 pr-6 pb-6 laptop:pr-10 laptop:pb-10 desktop:pr-10 desktop:pb-10">
            <div className={sectionBodyClassName}>
              <div className={sectionTitleClassName}>Schedule details</div>
              <div className="grid gap-4 md:grid-cols-2">
                <Input
                  id="schedule-name"
                  label="Schedule name"
                  initValue={values.name}
                  maxLength={100}
                  onChange={(value) => setNextValues((current) => ({ ...current, name: value }))}
                  onChangeBlur={() => handleBlur("name")}
                  error={getVisibleError("name")}
                  autoFocus
                />
                <Select
                  id="schedule-action"
                  label="Action type"
                  options={actionOptions}
                  value={values.action}
                  onChange={handleActionChange}
                />
              </div>
              {showPowerTargetFields ? (
                <Select
                  id="power-target-mode"
                  label="Power target"
                  options={powerTargetModeOptions}
                  value={values.powerTargetMode}
                  onChange={(value) =>
                    setNextValues((current) => ({
                      ...current,
                      powerTargetMode: value as ScheduleFormValues["powerTargetMode"],
                    }))
                  }
                />
              ) : null}
            </div>

            <div className={sectionBodyClassName}>
              <div className={sectionTitleClassName}>{showRecurringFields ? "Schedule" : "Date and time"}</div>
              <Select
                id="schedule-type"
                label="Type"
                options={scheduleTypeOptions}
                value={values.scheduleType}
                onChange={handleScheduleTypeChange}
              />

              {showRecurringFields ? (
                <>
                  <div
                    className={clsx("grid gap-4", {
                      "md:grid-cols-2": values.frequency === "weekly" || values.frequency === "monthly",
                    })}
                  >
                    <Select
                      id="schedule-frequency"
                      label="Frequency"
                      options={frequencyOptions}
                      value={values.frequency}
                      onChange={(value) =>
                        setNextValues((current) => ({
                          ...current,
                          frequency: value as ScheduleFormValues["frequency"],
                        }))
                      }
                    />
                    {values.frequency === "weekly" ? (
                      <WeekdaySelect
                        id="schedule-days-of-week"
                        value={values.daysOfWeek}
                        onChange={(daysOfWeek) => setNextValues((current) => ({ ...current, daysOfWeek }))}
                        error={getVisibleError("daysOfWeek")}
                      />
                    ) : null}
                    {values.frequency === "monthly" ? (
                      <Input
                        id="schedule-day-of-month"
                        label="Day of month"
                        type="number"
                        initValue={values.dayOfMonth}
                        onChange={(value) => setNextValues((current) => ({ ...current, dayOfMonth: value }))}
                        onChangeBlur={() => handleBlur("dayOfMonth")}
                        error={getVisibleError("dayOfMonth")}
                      />
                    ) : null}
                  </div>

                  <div className={clsx("grid gap-4", { "md:grid-cols-2": showPowerTargetWindow })}>
                    <Select
                      id="schedule-start-time"
                      label="Start time"
                      options={timeOptions}
                      value={values.startTime}
                      onChange={(value) => setNextValues((current) => ({ ...current, startTime: value }))}
                      error={getVisibleError("startTime")}
                    />
                    {showPowerTargetWindow ? (
                      <Select
                        id="schedule-end-time"
                        label="End time"
                        options={timeOptions}
                        value={values.endTime}
                        onChange={(value) => setNextValues((current) => ({ ...current, endTime: value }))}
                        error={getVisibleError("endTime")}
                      />
                    ) : null}
                  </div>

                  <div className="grid gap-4 md:grid-cols-2">
                    <DatePickerField
                      id="schedule-start-date"
                      label="Start date"
                      labelPlacement="floating"
                      selectedDate={selectedStartDate}
                      onSelectedDateChange={(date) => updateDateField("startDate", date)}
                      onBlur={() => touchDateField("startDate")}
                      onOpenChange={(open) => {
                        if (!open) {
                          touchDateField("startDate");
                        }
                      }}
                      error={getVisibleError("startDate")}
                      popoverRenderMode="portal-scrolling"
                      testId="schedule-start-date"
                    />
                    <Select
                      id="schedule-end-behavior"
                      label="End behavior"
                      options={endBehaviorOptions}
                      value={values.endBehavior}
                      onChange={(value) =>
                        setNextValues((current) => ({
                          ...current,
                          endBehavior: value as ScheduleFormValues["endBehavior"],
                        }))
                      }
                    />
                  </div>

                  {values.endBehavior === "endDate" ? (
                    <div className="grid gap-4 md:grid-cols-2">
                      <DatePickerField
                        id="schedule-end-date"
                        label="End date"
                        labelPlacement="floating"
                        selectedDate={selectedEndDate}
                        onSelectedDateChange={(date) => updateDateField("endDate", date)}
                        onBlur={() => touchDateField("endDate")}
                        onOpenChange={(open) => {
                          if (!open) {
                            touchDateField("endDate");
                          }
                        }}
                        isDateDisabled={isEndDateDisabled}
                        error={getVisibleError("endDate")}
                        popoverRenderMode="portal-scrolling"
                        testId="schedule-end-date"
                      />
                    </div>
                  ) : null}
                </>
              ) : (
                <div className="grid gap-4 md:grid-cols-2">
                  <DatePickerField
                    id="schedule-start-date"
                    label="Start date"
                    labelPlacement="floating"
                    selectedDate={selectedStartDate}
                    onSelectedDateChange={(date) => updateDateField("startDate", date)}
                    onBlur={() => touchDateField("startDate")}
                    onOpenChange={(open) => {
                      if (!open) {
                        touchDateField("startDate");
                      }
                    }}
                    error={getVisibleError("startDate")}
                    popoverRenderMode="portal-scrolling"
                    testId="schedule-start-date"
                  />
                  <Select
                    id="schedule-start-time"
                    label="Time"
                    options={timeOptions}
                    value={values.startTime}
                    onChange={(value) => setNextValues((current) => ({ ...current, startTime: value }))}
                    error={getVisibleError("startTime")}
                  />
                </div>
              )}

              <div className="text-200 text-text-primary-70">
                {isEditMode ? formatTimezoneLabel(values.timezone) : formatClientTimezoneLabel()}
              </div>
            </div>

            <div className={sectionBodyClassName}>
              <div className={sectionTitleClassName}>Apply to</div>
              <div className="grid gap-4 md:grid-cols-3">
                <TargetSelectButton
                  label="Racks"
                  value={getTargetButtonLabel(validRackTargetCount, "rack")}
                  onClick={() => setShowRackSelectionModal(true)}
                />
                <TargetSelectButton
                  label="Groups"
                  value={getTargetButtonLabel(validGroupTargetCount, "group")}
                  onClick={() => setShowGroupSelectionModal(true)}
                />
                <TargetSelectButton
                  label="Miners"
                  value={getTargetButtonLabel(validMinerTargetCount, "miner")}
                  onClick={() => setShowMinerSelectionModal(true)}
                />
              </div>
            </div>
          </section>
        }
        secondaryPane={<SchedulePreview values={values} isEditMode={isEditMode} />}
      />

      {showRackSelectionModal ? (
        <RackSelectionModal
          open={showRackSelectionModal}
          selectedRackIds={values.rackTargetIds}
          onDismiss={() => setShowRackSelectionModal(false)}
          onSave={(rackTargetIds) => {
            setNextValues((current) => ({ ...current, rackTargetIds }));
            setShowRackSelectionModal(false);
          }}
        />
      ) : null}

      {showGroupSelectionModal ? (
        <GroupSelectionModal
          open={showGroupSelectionModal}
          selectedGroupIds={values.groupTargetIds}
          onDismiss={() => setShowGroupSelectionModal(false)}
          onSave={(groupTargetIds) => {
            setNextValues((current) => ({ ...current, groupTargetIds }));
            setShowGroupSelectionModal(false);
          }}
        />
      ) : null}

      {showMinerSelectionModal ? (
        <MinerSelectionModal
          open={showMinerSelectionModal}
          selectedMinerIds={values.minerTargetIds}
          onDismiss={() => setShowMinerSelectionModal(false)}
          onSave={(minerTargetIds) => {
            setNextValues((current) => ({ ...current, minerTargetIds }));
            setShowMinerSelectionModal(false);
          }}
        />
      ) : null}
    </>
  );
};

export default ScheduleModal;
