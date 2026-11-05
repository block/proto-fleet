import {
  type KeyboardEvent,
  type ReactElement,
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import clsx from "clsx";

import { AutomationResponseProfileRevisionConflictError } from "@/protoFleet/api/automationResponseProfileRevisionConflict";
import { formatCurtailmentDuration } from "@/protoFleet/api/curtailmentDisplayUtils";
import {
  describeCurtailmentSchedule,
  describeNextScheduleWindow,
  formatScheduleDate,
} from "@/protoFleet/api/curtailmentSchedules";
import type {
  AutomationRule,
  CurtailmentScheduleDefinition,
  ResponseProfile,
  ScheduleRuleFormValues,
} from "@/protoFleet/api/curtailmentTypes";
import type { CurtailmentScheduleDayOfWeek } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { DayOfWeek } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { automationRuleTableClassName } from "@/protoFleet/features/settings/components/Curtailment/automationRuleTable";
import {
  InfoToggleContent,
  SectionDependencyMessage,
  SectionHeader,
} from "@/protoFleet/features/settings/components/Curtailment/CurtailmentSection";
import { isInputEnterSaveEvent } from "@/protoFleet/features/settings/components/Curtailment/keyboard";
import {
  getDefaultScheduleRuleFormValues,
  type ScheduleRuleFormErrors,
  validateScheduleRuleFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/scheduleRuleValidation";
import {
  getScheduleWindowTiming,
  type ScheduleWindowTiming,
} from "@/protoFleet/features/settings/components/Curtailment/scheduleWindowTiming";
import {
  formatClientTimezoneLabel,
  formatTimezoneLabel,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import {
  frequencyOptions,
  scheduleTypeOptions,
} from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";
import WeekdaySelect from "@/protoFleet/features/settings/components/Schedules/WeekdaySelect";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import { formatDateValue, parseDate } from "@/protoFleet/features/settings/utils/scheduleDateUtils";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select, { type SelectOption } from "@/shared/components/Select";
import Switch from "@/shared/components/Switch";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const SCHEDULES_DESCRIPTION = "Times that automatically trigger a response profile.";
const sectionLabelClassName = "text-200 font-semibold tracking-[0.08em] text-text-primary-50 uppercase";

const scheduleCols = {
  name: "name",
  schedule: "schedule",
  nextWindow: "nextWindow",
  responseProfile: "responseProfile",
  enabled: "enabled",
} as const;

type ScheduleColumn = (typeof scheduleCols)[keyof typeof scheduleCols];

const activeScheduleCols: ScheduleColumn[] = [
  scheduleCols.name,
  scheduleCols.schedule,
  scheduleCols.nextWindow,
  scheduleCols.responseProfile,
  scheduleCols.enabled,
];

const phoneScheduleCols: ScheduleColumn[] = [scheduleCols.name, scheduleCols.schedule, scheduleCols.enabled];

const scheduleColTitles: ColTitles<ScheduleColumn> = {
  name: "Name",
  schedule: "Schedule",
  nextWindow: "Next window",
  responseProfile: "Response profile",
  enabled: "",
};

const scheduleColumnAriaLabels: Partial<Record<ScheduleColumn, string>> = {
  enabled: "Enabled",
};

const scheduleColumnsExemptFromDisabledStyling = new Set<ScheduleColumn>([scheduleCols.enabled]);

const emptyScheduleRules: AutomationRule[] = [];
const emptyResponseProfiles: ResponseProfile[] = [];

type ScheduleRuleWithDetails = AutomationRule & {
  responseProfileName: string;
  isResponseProfileAutomationReady: boolean;
};

type CurtailmentSchedulesContentProps = {
  initialScheduleRules?: AutomationRule[];
  scheduleRules?: AutomationRule[];
  responseProfiles?: ResponseProfile[];
  isLoading?: boolean;
  loadError?: string | null;
  isCreating?: boolean;
  updatingRuleIds?: ReadonlySet<string>;
  isLoadingResponseProfiles?: boolean;
  loadResponseProfilesError?: string | null;
  onCreateSchedule?: (values: ScheduleRuleFormValues) => Promise<AutomationRule | void>;
  onUpdateSchedule?: (rule: AutomationRule, values: ScheduleRuleFormValues) => Promise<AutomationRule | void>;
  onToggleSchedule?: (
    rule: AutomationRule,
    enabled: boolean,
    expectedResponseProfileRevision?: string,
  ) => Promise<AutomationRule | void>;
  onDeleteSchedule?: (rule: AutomationRule) => Promise<void>;
};

type ScheduleRuleModalProps = {
  open: boolean;
  mode: "create" | "edit";
  initialValues: ScheduleRuleFormValues;
  responseProfiles: ResponseProfile[];
  isLoadingResponseProfiles?: boolean;
  loadResponseProfilesError?: string | null;
  saving?: boolean;
  deleting?: boolean;
  onDismiss: () => void;
  onSave: (values: ScheduleRuleFormValues) => Promise<void>;
  onDelete?: () => Promise<void>;
};

// The miner and curtailment protos both number days SUNDAY = 1 through SATURDAY = 7.
const toWeekdaySelectValue = (days: CurtailmentScheduleDayOfWeek[]) => days.map((day) => day as number as DayOfWeek);
const fromWeekdaySelectValue = (days: DayOfWeek[]) => days.map((day) => day as number as CurtailmentScheduleDayOfWeek);

function getScheduleRuleFormValues(
  rule: AutomationRule | null,
  responseProfiles: ResponseProfile[],
): ScheduleRuleFormValues {
  const defaults = getDefaultScheduleRuleFormValues(responseProfiles);
  if (!rule?.schedule) {
    return defaults;
  }
  return {
    name: rule.name,
    responseProfileId: rule.responseProfileId,
    responseProfileRevision: responseProfiles.find((profile) => profile.id === rule.responseProfileId)?.revision ?? "",
    schedule: {
      ...rule.schedule,
      endDate: rule.schedule.endDate || rule.schedule.startDate,
      dayOfMonth: rule.schedule.dayOfMonth || defaults.schedule.dayOfMonth,
    },
  };
}

function createOption(profile: ResponseProfile): SelectOption {
  return { value: profile.id, label: profile.name };
}

function SchedulesInfoToggle(): ReactElement {
  return (
    <PopoverProvider>
      <InfoToggleContent
        ariaLabel="About schedules"
        description={SCHEDULES_DESCRIPTION}
        testId="curtailment-schedules-info-popover"
        triggerClassName="curtailment-schedules-info-trigger"
      />
    </PopoverProvider>
  );
}

function ScheduleInfoNote({ children, className }: { children: ReactNode; className?: string }): ReactElement {
  return (
    <p className={clsx("flex items-start gap-1.5 text-200 text-intent-info-text", className)}>
      <Info width={iconSizes.small} className="mt-0.5 shrink-0" />
      {children}
    </p>
  );
}

const formatWindowDuration = (durationMs: number) =>
  formatCurtailmentDuration(durationMs / 1000, { style: "estimate", approximate: false });

function ScheduleWindowDuration({
  timing,
  isRecurring,
}: {
  timing: ScheduleWindowTiming;
  isRecurring: boolean;
}): ReactElement {
  const { clockDurationMs, clockChange } = timing;
  const durationMs = isRecurring ? clockDurationMs : (clockChange?.durationMs ?? clockDurationMs);
  const shiftMs = clockChange ? clockChange.durationMs - clockDurationMs : 0;
  const shift = `${formatWindowDuration(Math.abs(shiftMs))} ${shiftMs > 0 ? "longer" : "shorter"}`;
  return (
    <div>
      <p className="text-300 text-text-primary-70" data-testid="schedule-rule-duration">
        Curtails for <span className="text-emphasis-300 text-text-primary">{formatWindowDuration(durationMs)}</span>
        {isRecurring ? " each time." : "."}
      </p>
      <div role="status">
        {clockChange ? (
          <ScheduleInfoNote className="mt-2">
            {isRecurring
              ? `A daylight saving time change makes the window starting ${formatScheduleDate(clockChange.startDate)} run ${shift}.`
              : `A daylight saving time change makes this window run ${shift} than its start and end times suggest.`}
          </ScheduleInfoNote>
        ) : null}
      </div>
    </div>
  );
}

function getSchedulesEmptyState(loadError: string | null, isLoading: boolean): ReactElement {
  if (loadError) {
    return <SettingsEmptyState size="section" title="Unable to load schedules" description={loadError} />;
  }
  if (isLoading) {
    return (
      <div className="flex min-h-[220px] w-full items-center justify-center py-14">
        <ProgressCircular indeterminate />
      </div>
    );
  }
  return (
    <SettingsEmptyState
      size="section"
      title="No schedules configured"
      description="Add a schedule to curtail at planned times."
    />
  );
}

function ScheduleRuleModal({
  open,
  mode,
  initialValues,
  responseProfiles,
  isLoadingResponseProfiles = false,
  loadResponseProfilesError = null,
  saving = false,
  deleting = false,
  onDismiss,
  onSave,
  onDelete,
}: ScheduleRuleModalProps): ReactElement {
  const [values, setValues] = useState<ScheduleRuleFormValues>(() => initialValues);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [showValidationErrors, setShowValidationErrors] = useState(false);
  const { schedule } = values;
  const responseProfileOptions = useMemo(() => responseProfiles.map(createOption), [responseProfiles]);
  const isBusy = saving || deleting;
  const validationErrors = useMemo(() => validateScheduleRuleFormValues(values), [values]);
  const visibleErrors: ScheduleRuleFormErrors = showValidationErrors ? validationErrors : {};
  const hasValidationErrors = Object.keys(validationErrors).length > 0;
  const isSelectedResponseProfileUnavailable =
    values.responseProfileId !== "" &&
    !responseProfiles.some((profile) => profile.id === values.responseProfileId && profile.revision);
  const isSaveUnavailable = isLoadingResponseProfiles || isSelectedResponseProfileUnavailable || isBusy;
  const isRecurring = schedule.scheduleType === "recurring";
  const crossesMidnight = isRecurring && schedule.endTime !== "" && schedule.endTime < schedule.startTime;
  const windowTiming = useMemo(() => getScheduleWindowTiming(schedule), [schedule]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- response profiles can load after the modal opens
    setValues((currentValues) => {
      const responseProfileId = currentValues.responseProfileId || responseProfiles[0]?.id || "";
      const responseProfile = responseProfiles.find((profile) => profile.id === responseProfileId);
      return {
        ...currentValues,
        responseProfileId,
        responseProfileRevision:
          currentValues.responseProfileId === responseProfileId && currentValues.responseProfileRevision
            ? currentValues.responseProfileRevision
            : (responseProfile?.revision ?? ""),
      };
    });
  }, [responseProfiles]);

  const updateSchedule = useCallback((patch: Partial<CurtailmentScheduleDefinition>) => {
    setValues((currentValues) => ({ ...currentValues, schedule: { ...currentValues.schedule, ...patch } }));
  }, []);

  const handleSave = useCallback(async () => {
    if (isSaveUnavailable) {
      return;
    }
    if (hasValidationErrors) {
      setShowValidationErrors(true);
      return;
    }

    try {
      setSaveError(null);
      await onSave(values);
      onDismiss();
    } catch (error) {
      if (error instanceof AutomationResponseProfileRevisionConflictError) {
        const latestRevision = error.latestResponseProfileRevisionById.get(values.responseProfileId) ?? "";
        setValues((currentValues) => ({ ...currentValues, responseProfileRevision: latestRevision }));
      }
      setSaveError(error instanceof Error && error.message ? error.message : "Failed to save schedule.");
    }
  }, [hasValidationErrors, isSaveUnavailable, onDismiss, onSave, values]);

  const handleFormKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (!isInputEnterSaveEvent(event)) {
        return;
      }
      event.preventDefault();
      void handleSave();
    },
    [handleSave],
  );

  const handleDelete = useCallback(async () => {
    if (!onDelete || isBusy) {
      return;
    }
    try {
      setSaveError(null);
      await onDelete();
      onDismiss();
    } catch (error) {
      setSaveError(error instanceof Error && error.message ? error.message : "Failed to delete schedule.");
    }
  }, [isBusy, onDelete, onDismiss]);

  const modalButtons = [
    ...(mode === "edit" && onDelete
      ? [
          {
            text: "Delete",
            variant: variants.secondaryDanger,
            disabled: isBusy,
            loading: deleting,
            dismissModalOnClick: false,
            onClick: () => void handleDelete(),
          },
        ]
      : []),
    {
      text: "Save",
      variant: variants.primary,
      disabled: isSaveUnavailable,
      loading: saving,
      dismissModalOnClick: false,
      onClick: () => void handleSave(),
    },
  ];

  const startDateField = (
    <DatePickerField
      id="schedule-rule-start-date"
      label="Start date"
      labelPlacement="floating"
      selectedDate={parseDate(schedule.startDate) ?? undefined}
      onSelectedDateChange={(date) => updateSchedule({ startDate: formatDateValue(date) })}
      error={visibleErrors.startDate}
      withInputs="date"
      popoverRenderMode="portal-scrolling"
      testId="schedule-rule-start-date"
    />
  );
  const startTimeField = (
    <Input
      id="schedule-rule-start-time"
      label="Start time"
      type="time"
      initValue={schedule.startTime}
      onChange={(startTime) => updateSchedule({ startTime })}
      error={visibleErrors.startTime}
      testId="schedule-rule-start-time"
    />
  );
  const endTimeField = (
    <Input
      id="schedule-rule-end-time"
      label="End time"
      type="time"
      initValue={schedule.endTime}
      onChange={(endTime) => updateSchedule({ endTime })}
      error={visibleErrors.endTime}
      testId="schedule-rule-end-time"
    />
  );

  return (
    <Modal
      open={open}
      title={mode === "edit" ? "Edit schedule" : "Create schedule"}
      description={SCHEDULES_DESCRIPTION}
      onDismiss={onDismiss}
      size={modalSizes.standard}
      divider={false}
      testId="curtailment-schedule-modal"
      buttons={modalButtons}
      bodyClassName="text-text-primary"
    >
      <div className="grid gap-6 pb-2" onKeyDown={handleFormKeyDown}>
        {saveError ? (
          <div className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical">{saveError}</div>
        ) : null}
        <Input
          id="schedule-rule-name"
          label="Schedule name"
          initValue={values.name}
          error={visibleErrors.name}
          onChange={(name) => setValues((currentValues) => ({ ...currentValues, name }))}
        />

        <div className="grid gap-3">
          <div className={sectionLabelClassName}>When</div>
          <Select
            id="schedule-rule-type"
            label="Type"
            options={scheduleTypeOptions}
            value={schedule.scheduleType}
            onChange={(scheduleType) =>
              updateSchedule({ scheduleType: scheduleType as CurtailmentScheduleDefinition["scheduleType"] })
            }
            testId="schedule-rule-type"
          />
          {isRecurring ? (
            <>
              <div
                className={clsx("grid gap-3", {
                  "tablet:grid-cols-2": schedule.frequency === "weekly" || schedule.frequency === "monthly",
                })}
              >
                <Select
                  id="schedule-rule-frequency"
                  label="Frequency"
                  options={frequencyOptions}
                  value={schedule.frequency}
                  onChange={(frequency) =>
                    updateSchedule({ frequency: frequency as CurtailmentScheduleDefinition["frequency"] })
                  }
                  testId="schedule-rule-frequency"
                />
                {schedule.frequency === "weekly" ? (
                  <WeekdaySelect
                    id="schedule-rule-days-of-week"
                    value={toWeekdaySelectValue(schedule.daysOfWeek)}
                    onChange={(days) => updateSchedule({ daysOfWeek: fromWeekdaySelectValue(days) })}
                    error={visibleErrors.daysOfWeek}
                  />
                ) : null}
                {schedule.frequency === "monthly" ? (
                  <Input
                    id="schedule-rule-day-of-month"
                    label="Day of month"
                    type="number"
                    inputMode="numeric"
                    initValue={schedule.dayOfMonth}
                    onChange={(dayOfMonth) => updateSchedule({ dayOfMonth })}
                    error={visibleErrors.dayOfMonth}
                  />
                ) : null}
              </div>
              <div className="grid gap-3 tablet:grid-cols-2">
                {startTimeField}
                <div>
                  {endTimeField}
                  <div role="status">
                    {crossesMidnight ? <ScheduleInfoNote className="mt-2">Ends the next day.</ScheduleInfoNote> : null}
                  </div>
                </div>
              </div>
              <div className="grid gap-3 tablet:grid-cols-2">
                {startDateField}
                <DatePickerField
                  id="schedule-rule-until-date"
                  label="Repeat until (optional)"
                  labelPlacement="floating"
                  selectedDate={parseDate(schedule.untilDate) ?? undefined}
                  onSelectedDateChange={(date) => updateSchedule({ untilDate: formatDateValue(date) })}
                  clearable
                  onClear={() => updateSchedule({ untilDate: "" })}
                  error={visibleErrors.untilDate}
                  withInputs="date"
                  popoverRenderMode="portal-scrolling"
                  testId="schedule-rule-until-date"
                />
              </div>
            </>
          ) : (
            <>
              <div className="grid gap-3 tablet:grid-cols-2">
                {startDateField}
                {startTimeField}
              </div>
              <div className="grid gap-3 tablet:grid-cols-2">
                <DatePickerField
                  id="schedule-rule-end-date"
                  label="End date"
                  labelPlacement="floating"
                  selectedDate={parseDate(schedule.endDate) ?? undefined}
                  onSelectedDateChange={(date) => updateSchedule({ endDate: formatDateValue(date) })}
                  error={visibleErrors.endDate}
                  withInputs="date"
                  popoverRenderMode="portal-scrolling"
                  testId="schedule-rule-end-date"
                />
                {endTimeField}
              </div>
            </>
          )}
          {windowTiming ? <ScheduleWindowDuration timing={windowTiming} isRecurring={isRecurring} /> : null}
          <div className="text-200 text-text-primary-70">
            {mode === "edit" ? formatTimezoneLabel(schedule.timezone) : formatClientTimezoneLabel()}
          </div>
        </div>

        <div className="grid gap-3">
          <div className={sectionLabelClassName}>Then</div>
          <div>
            <Select
              id="schedule-rule-response-profile"
              label="Response profile"
              options={responseProfileOptions}
              value={values.responseProfileId}
              onChange={(responseProfileId) => {
                const responseProfileRevision =
                  responseProfiles.find((profile) => profile.id === responseProfileId)?.revision ?? "";
                setValues((currentValues) => ({ ...currentValues, responseProfileId, responseProfileRevision }));
              }}
              disabled={isLoadingResponseProfiles || responseProfileOptions.length === 0}
              error={visibleErrors.responseProfileId}
              testId="schedule-rule-response-profile-select"
              forceBelow
            />
            {loadResponseProfilesError ? (
              <SectionDependencyMessage>Unable to load response profiles.</SectionDependencyMessage>
            ) : null}
            {!isLoadingResponseProfiles && !loadResponseProfilesError && responseProfileOptions.length === 0 ? (
              <SectionDependencyMessage>No response profiles available.</SectionDependencyMessage>
            ) : null}
          </div>
        </div>
      </div>
    </Modal>
  );
}

function createScheduleColConfig(
  onToggle: (ruleId: string) => void,
  updatingRuleIds: ReadonlySet<string>,
): ColConfig<ScheduleRuleWithDetails, string, ScheduleColumn> {
  return {
    [scheduleCols.name]: {
      allowWrap: true,
      component: (rule) => (
        <div>
          <span className="block max-w-full truncate text-emphasis-300 text-text-primary">{rule.name}</span>
          {rule.bindingStatus === "rebind_required" ? (
            <div className="text-200 wrap-anywhere whitespace-normal text-text-critical">
              Profile changed. Edit this schedule and save the current profile to resume.
            </div>
          ) : rule.lastError ? (
            <div className="text-200 wrap-anywhere whitespace-normal text-text-critical">{rule.lastError}</div>
          ) : null}
        </div>
      ),
      width: "w-[24%] phone:w-auto",
    },
    [scheduleCols.schedule]: {
      allowWrap: true,
      component: (rule) => <span className="text-text-primary">{rule.conditionSummary}</span>,
      width: "w-[30%] phone:w-auto",
    },
    [scheduleCols.nextWindow]: {
      component: (rule) => <span className="truncate text-text-primary">{describeNextScheduleWindow(rule)}</span>,
      width: "w-[20%] phone:w-auto",
    },
    [scheduleCols.responseProfile]: {
      component: (rule) => <span className="truncate text-text-primary">{rule.responseProfileName}</span>,
      width: "w-[20%] phone:w-auto",
    },
    [scheduleCols.enabled]: {
      component: (rule) => (
        <div className="flex justify-end" data-interactive>
          <Switch
            checked={rule.enabled}
            setChecked={() => onToggle(rule.id)}
            disabled={updatingRuleIds.has(rule.id) || (!rule.enabled && !rule.isResponseProfileAutomationReady)}
          />
        </div>
      ),
      width: "w-[6%] phone:w-14",
    },
  };
}

function createLocalScheduleRule(values: ScheduleRuleFormValues, id: string, priority: number): AutomationRule {
  return {
    id,
    priority,
    name: values.name.trim(),
    conditionType: "schedule",
    conditionSummary: describeCurtailmentSchedule(values.schedule),
    schedule: values.schedule,
    responseProfileId: values.responseProfileId,
    enabled: true,
  };
}

export function CurtailmentSchedulesContent({
  initialScheduleRules = emptyScheduleRules,
  scheduleRules: controlledScheduleRules,
  responseProfiles = emptyResponseProfiles,
  isLoading = false,
  loadError = null,
  isCreating = false,
  updatingRuleIds = new Set<string>(),
  isLoadingResponseProfiles = false,
  loadResponseProfilesError = null,
  onCreateSchedule,
  onUpdateSchedule,
  onToggleSchedule,
  onDeleteSchedule,
}: CurtailmentSchedulesContentProps): ReactElement {
  const [localScheduleRules, setLocalScheduleRules] = useState<AutomationRule[]>(() => [...initialScheduleRules]);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<AutomationRule | null>(null);
  const { isPhone } = useWindowDimensions();
  const scheduleRules = controlledScheduleRules ?? localScheduleRules;
  const automationReadyResponseProfiles = useMemo(
    () => responseProfiles.filter((profile) => profile.isAutomationReady && profile.revision),
    [responseProfiles],
  );

  const rulesWithDetails = useMemo<ScheduleRuleWithDetails[]>(() => {
    const responseProfilesById = new Map(responseProfiles.map((profile) => [profile.id, profile]));
    return scheduleRules.map((rule) => {
      const responseProfile = responseProfilesById.get(rule.responseProfileId);
      return {
        ...rule,
        responseProfileName: responseProfile?.name ?? rule.responseProfileName ?? "Unknown profile",
        isResponseProfileAutomationReady: responseProfile?.isAutomationReady === true,
      };
    });
  }, [responseProfiles, scheduleRules]);

  const modalMode = editingRule ? "edit" : "create";
  const modalInitialValues = useMemo(
    () => getScheduleRuleFormValues(editingRule, automationReadyResponseProfiles),
    [automationReadyResponseProfiles, editingRule],
  );

  const openCreateModal = useCallback(() => {
    setEditingRule(null);
    setIsModalOpen(true);
  }, []);

  const openEditModal = useCallback((rule: AutomationRule) => {
    setEditingRule(rule);
    setIsModalOpen(true);
  }, []);

  const closeModal = useCallback(() => {
    setIsModalOpen(false);
    setEditingRule(null);
  }, []);

  const toggleSchedule = useCallback(
    (ruleId: string) => {
      const rule = scheduleRules.find((currentRule) => currentRule.id === ruleId);
      const responseProfileRevision = automationReadyResponseProfiles.find(
        (profile) => profile.id === rule?.responseProfileId,
      )?.revision;
      if (!rule || updatingRuleIds.has(ruleId) || (!rule.enabled && !responseProfileRevision)) {
        return;
      }

      const nextEnabled = !rule.enabled;
      if (onToggleSchedule) {
        void onToggleSchedule(rule, nextEnabled, nextEnabled ? responseProfileRevision : undefined).catch(() => {});
        return;
      }
      setLocalScheduleRules((currentRules) =>
        currentRules.map((currentRule) =>
          currentRule.id === ruleId ? { ...currentRule, enabled: nextEnabled } : currentRule,
        ),
      );
    },
    [automationReadyResponseProfiles, onToggleSchedule, scheduleRules, updatingRuleIds],
  );

  const colConfig = useMemo(
    () => createScheduleColConfig(toggleSchedule, updatingRuleIds),
    [toggleSchedule, updatingRuleIds],
  );

  const handleSave = useCallback(
    async (values: ScheduleRuleFormValues) => {
      const savedRule = editingRule ? await onUpdateSchedule?.(editingRule, values) : await onCreateSchedule?.(values);
      if (controlledScheduleRules) {
        return;
      }
      const nextRule =
        savedRule ||
        (editingRule
          ? { ...createLocalScheduleRule(values, editingRule.id, editingRule.priority), enabled: editingRule.enabled }
          : createLocalScheduleRule(values, `schedule-${scheduleRules.length + 1}`, scheduleRules.length + 1));
      setLocalScheduleRules((currentRules) => [
        ...currentRules.filter((currentRule) => currentRule.id !== nextRule.id),
        nextRule,
      ]);
    },
    [controlledScheduleRules, editingRule, onCreateSchedule, onUpdateSchedule, scheduleRules.length],
  );

  const handleDelete = useCallback(async () => {
    if (!editingRule) {
      return;
    }
    await onDeleteSchedule?.(editingRule);
    if (!controlledScheduleRules) {
      setLocalScheduleRules((currentRules) => currentRules.filter((rule) => rule.id !== editingRule.id));
    }
  }, [controlledScheduleRules, editingRule, onDeleteSchedule]);

  const isEditingBusy = editingRule ? updatingRuleIds.has(editingRule.id) : false;

  return (
    <section className={clsx("curtailment-settings__section", scheduleRules.length === 0 && "min-h-[300px]")}>
      <SectionHeader
        title="Schedules"
        buttonText="Create schedule"
        onButtonClick={openCreateModal}
        infoToggle={<SchedulesInfoToggle />}
      />
      <List<ScheduleRuleWithDetails, string, ScheduleColumn>
        activeCols={isPhone ? phoneScheduleCols : activeScheduleCols}
        colTitles={scheduleColTitles}
        columnHeaderAriaLabels={scheduleColumnAriaLabels}
        colConfig={colConfig}
        items={rulesWithDetails}
        itemKey="id"
        total={rulesWithDetails.length}
        hideTotal
        itemName={{ singular: "schedule", plural: "schedules" }}
        stickyFirstColumn={false}
        isRowDisabled={(rule) => !rule.enabled || !rule.isResponseProfileAutomationReady}
        columnsExemptFromDisabledStyling={scheduleColumnsExemptFromDisabledStyling}
        tableClassName={automationRuleTableClassName}
        noDataElement={getSchedulesEmptyState(loadError, isLoading)}
        applyColumnWidthsToCells
        onRowClick={openEditModal}
      />

      <ScheduleRuleModal
        key={isModalOpen ? `schedule-modal-${modalMode}-${editingRule?.id ?? "new"}` : "schedule-modal-closed"}
        open={isModalOpen}
        mode={modalMode}
        initialValues={modalInitialValues}
        responseProfiles={automationReadyResponseProfiles}
        isLoadingResponseProfiles={isLoadingResponseProfiles}
        loadResponseProfilesError={loadResponseProfilesError}
        saving={editingRule ? isEditingBusy : isCreating}
        deleting={isEditingBusy}
        onDismiss={closeModal}
        onSave={handleSave}
        onDelete={editingRule ? handleDelete : undefined}
      />
    </section>
  );
}

export default CurtailmentSchedulesContent;
