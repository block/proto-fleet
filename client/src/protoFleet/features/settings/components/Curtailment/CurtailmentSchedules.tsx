import { type ReactElement, type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

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
import {
  automationRuleModalSectionLabelClassName,
  AutomationRuleResponseProfileField,
  AutomationRuleSaveError,
} from "@/protoFleet/features/settings/components/Curtailment/automationRuleModal";
import {
  automationRuleEnabledColumn,
  automationRuleListEmptyState,
  automationRuleNameColumn,
  automationRuleTableClassName,
  type AutomationRuleWithDetails,
  emptyUpdatingRuleIds,
  withResponseProfileDetails,
} from "@/protoFleet/features/settings/components/Curtailment/automationRuleTable";
import {
  InfoToggleContent,
  SectionHeader,
} from "@/protoFleet/features/settings/components/Curtailment/CurtailmentSection";
import {
  getDefaultScheduleRuleFormValues,
  type ScheduleRuleFormErrors,
  validateScheduleRuleFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/scheduleRuleValidation";
import {
  getScheduleWindowTiming,
  type ScheduleWindowTiming,
} from "@/protoFleet/features/settings/components/Curtailment/scheduleWindowTiming";
import { useAutomationRuleModalActions } from "@/protoFleet/features/settings/components/Curtailment/useAutomationRuleModalActions";
import {
  formatClientTimezoneLabel,
  formatTimezoneLabel,
} from "@/protoFleet/features/settings/components/Schedules/constants";
import {
  frequencyOptions,
  scheduleTypeOptions,
} from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";
import WeekdaySelect from "@/protoFleet/features/settings/components/Schedules/WeekdaySelect";
import { formatDateValue, parseDate } from "@/protoFleet/features/settings/utils/scheduleDateUtils";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";
import Select from "@/shared/components/Select";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const SCHEDULES_DESCRIPTION = "Times that automatically trigger a response profile.";

const scheduleRuleCols = {
  name: "name",
  schedule: "schedule",
  nextWindow: "nextWindow",
  responseProfile: "responseProfile",
  enabled: "enabled",
} as const;

type ScheduleRuleColumn = (typeof scheduleRuleCols)[keyof typeof scheduleRuleCols];

const activeScheduleRuleCols: ScheduleRuleColumn[] = [
  scheduleRuleCols.name,
  scheduleRuleCols.schedule,
  scheduleRuleCols.nextWindow,
  scheduleRuleCols.responseProfile,
  scheduleRuleCols.enabled,
];

const phoneScheduleRuleCols: ScheduleRuleColumn[] = [
  scheduleRuleCols.name,
  scheduleRuleCols.schedule,
  scheduleRuleCols.enabled,
];

const scheduleRuleColTitles: ColTitles<ScheduleRuleColumn> = {
  name: "Name",
  schedule: "Schedule",
  nextWindow: "Next window",
  responseProfile: "Response profile",
  enabled: "",
};

const scheduleRuleColumnAriaLabels: Partial<Record<ScheduleRuleColumn, string>> = {
  enabled: "Enabled",
};

const scheduleRuleColumnsExemptFromDisabledStyling = new Set<ScheduleRuleColumn>([scheduleRuleCols.enabled]);

const emptyScheduleRules: AutomationRule[] = [];
const emptyResponseProfiles: ResponseProfile[] = [];

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
  const [showValidationErrors, setShowValidationErrors] = useState(false);
  const { schedule } = values;
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

  const { saveError, handleFormKeyDown, modalButtons } = useAutomationRuleModalActions({
    mode,
    ruleNoun: "schedule",
    values,
    setValues,
    setShowValidationErrors,
    hasValidationErrors,
    isSaveUnavailable,
    saving,
    deleting,
    onSave,
    onDelete,
    onDismiss,
  });

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
        <AutomationRuleSaveError message={saveError} />
        <Input
          id="schedule-rule-name"
          label="Schedule name"
          initValue={values.name}
          error={visibleErrors.name}
          onChange={(name) => setValues((currentValues) => ({ ...currentValues, name }))}
        />

        <div className="grid gap-3">
          <div className={automationRuleModalSectionLabelClassName}>When</div>
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
          <div className={automationRuleModalSectionLabelClassName}>Then</div>
          <AutomationRuleResponseProfileField
            id="schedule-rule-response-profile"
            testId="schedule-rule-response-profile-select"
            responseProfiles={responseProfiles}
            value={values.responseProfileId}
            onChange={(responseProfileId, responseProfileRevision) =>
              setValues((currentValues) => ({ ...currentValues, responseProfileId, responseProfileRevision }))
            }
            error={visibleErrors.responseProfileId}
            isLoading={isLoadingResponseProfiles}
            loadError={loadResponseProfilesError}
          />
        </div>
      </div>
    </Modal>
  );
}

function createScheduleRuleColConfig(
  onToggle: (ruleId: string) => void,
  updatingRuleIds: ReadonlySet<string>,
): ColConfig<AutomationRuleWithDetails, string, ScheduleRuleColumn> {
  return {
    [scheduleRuleCols.name]: automationRuleNameColumn("schedule", "w-[24%] phone:w-auto"),
    [scheduleRuleCols.schedule]: {
      allowWrap: true,
      component: (rule) => <span className="text-text-primary">{rule.conditionSummary}</span>,
      width: "w-[30%] phone:w-auto",
    },
    [scheduleRuleCols.nextWindow]: {
      component: (rule) => <span className="truncate text-text-primary">{describeNextScheduleWindow(rule)}</span>,
      width: "w-[20%] phone:w-auto",
    },
    [scheduleRuleCols.responseProfile]: {
      component: (rule) => <span className="truncate text-text-primary">{rule.responseProfileName}</span>,
      width: "w-[20%] phone:w-auto",
    },
    [scheduleRuleCols.enabled]: automationRuleEnabledColumn(onToggle, updatingRuleIds),
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
  updatingRuleIds = emptyUpdatingRuleIds,
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

  const rulesWithDetails = useMemo(
    () => withResponseProfileDetails(scheduleRules, responseProfiles),
    [responseProfiles, scheduleRules],
  );

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
    () => createScheduleRuleColConfig(toggleSchedule, updatingRuleIds),
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
      <List<AutomationRuleWithDetails, string, ScheduleRuleColumn>
        activeCols={isPhone ? phoneScheduleRuleCols : activeScheduleRuleCols}
        colTitles={scheduleRuleColTitles}
        columnHeaderAriaLabels={scheduleRuleColumnAriaLabels}
        colConfig={colConfig}
        items={rulesWithDetails}
        itemKey="id"
        total={rulesWithDetails.length}
        hideTotal
        itemName={{ singular: "schedule", plural: "schedules" }}
        stickyFirstColumn={false}
        isRowDisabled={(rule) => !rule.enabled || !rule.isResponseProfileAutomationReady}
        columnsExemptFromDisabledStyling={scheduleRuleColumnsExemptFromDisabledStyling}
        tableClassName={automationRuleTableClassName}
        noDataElement={automationRuleListEmptyState({
          loadError,
          isLoading,
          errorTitle: "Unable to load schedules",
          emptyTitle: "No schedules configured",
          emptyDescription: "Add a schedule to curtail at planned times.",
        })}
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
