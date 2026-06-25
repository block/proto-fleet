import { type KeyboardEvent, type ReactElement, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import { isInputEnterSaveEvent } from "@/protoFleet/features/settings/components/Curtailment/keyboard";
import type {
  AutomationRule,
  AutomationRuleFormValues,
  CurtailmentSource,
  ResponseProfile,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select, { type SelectOption } from "@/shared/components/Select";
import Switch from "@/shared/components/Switch";
import { positions } from "@/shared/constants";
import { classNameToSelectors } from "@/shared/utils/cssUtils";

const AUTOMATIONS_DESCRIPTION = "Conditions that automatically trigger a response profile.";
const GRID_SIGNAL_VALUE = "0";
const GRID_SIGNAL_HINT =
  "When the signal changes to 100, your selected response profile will begin the restore process.";

const automationCols = {
  name: "name",
  condition: "condition",
  responseProfile: "responseProfile",
  enabled: "enabled",
} as const;

type AutomationColumn = (typeof automationCols)[keyof typeof automationCols];

const activeAutomationCols: AutomationColumn[] = [
  automationCols.name,
  automationCols.condition,
  automationCols.responseProfile,
  automationCols.enabled,
];

const automationColTitles: ColTitles<AutomationColumn> = {
  name: "Name",
  condition: "Condition",
  responseProfile: "Response profile",
  enabled: "",
};

const automationColumnAriaLabels: Partial<Record<AutomationColumn, string>> = {
  enabled: "Enabled",
};

const automationColumnsExemptFromDisabledStyling = new Set<AutomationColumn>([automationCols.enabled]);

const automationTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "phone:[&_thead_th:last-child]:w-14",
  "phone:[&_thead_th:last-child>div]:w-14",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:box-border",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:flex",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:justify-end",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:w-14",
].join(" ");

const emptyAutomations: AutomationRule[] = [];
const emptySources: CurtailmentSource[] = [];
const emptyResponseProfiles: ResponseProfile[] = [];

type AutomationRuleWithDetails = AutomationRule & {
  responseProfileName: string;
};

type AutomationModalProps = {
  open: boolean;
  mode: "create" | "edit";
  initialValues: AutomationRuleFormValues;
  sources: CurtailmentSource[];
  responseProfiles: ResponseProfile[];
  isLoadingSources?: boolean;
  loadSourcesError?: string | null;
  isLoadingResponseProfiles?: boolean;
  loadResponseProfilesError?: string | null;
  saving?: boolean;
  deleting?: boolean;
  onDismiss: () => void;
  onSave: (values: AutomationRuleFormValues) => Promise<void>;
  onDelete?: () => Promise<void>;
};

type AutomationFormErrors = Partial<Record<keyof AutomationRuleFormValues, string>>;

type InfoToggleContentProps = {
  ariaLabel: string;
  description: string;
  testId: string;
  triggerClassName: string;
};

type SectionHeaderProps = {
  title: string;
  buttonText: string;
  onButtonClick: () => void;
  infoToggle?: ReactElement;
};

type CurtailmentAutomationsContentProps = {
  initialAutomationRules?: AutomationRule[];
  automationRules?: AutomationRule[];
  sources?: CurtailmentSource[];
  responseProfiles?: ResponseProfile[];
  isLoading?: boolean;
  loadError?: string | null;
  isCreating?: boolean;
  updatingRuleIds?: ReadonlySet<string>;
  isLoadingSources?: boolean;
  loadSourcesError?: string | null;
  isLoadingResponseProfiles?: boolean;
  loadResponseProfilesError?: string | null;
  onCreateAutomation?: (values: AutomationRuleFormValues) => Promise<AutomationRule | void>;
  onUpdateAutomation?: (rule: AutomationRule, values: AutomationRuleFormValues) => Promise<AutomationRule | void>;
  onToggleAutomation?: (rule: AutomationRule, enabled: boolean) => Promise<AutomationRule | void>;
  onDeleteAutomation?: (rule: AutomationRule) => Promise<void>;
};

function createAutomationRuleId(name: string, existingRules: AutomationRule[]): string {
  const baseSlug =
    name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "automation";
  const existingIds = new Set(existingRules.map((rule) => rule.id));

  let candidate = baseSlug;
  let suffix = 2;
  while (existingIds.has(candidate)) {
    candidate = `${baseSlug}-${suffix}`;
    suffix += 1;
  }

  return candidate;
}

function getDefaultAutomationFormValues(
  sources: CurtailmentSource[],
  responseProfiles: ResponseProfile[],
): AutomationRuleFormValues {
  return {
    name: "",
    sourceId: sources[0]?.id ?? "",
    responseProfileId: responseProfiles[0]?.id ?? "",
  };
}

function getAutomationFormValuesFromRule(
  rule: AutomationRule | null,
  sources: CurtailmentSource[],
  responseProfiles: ResponseProfile[],
): AutomationRuleFormValues {
  if (!rule) {
    return getDefaultAutomationFormValues(sources, responseProfiles);
  }

  return {
    name: rule.name,
    sourceId: rule.sourceId ?? sources[0]?.id ?? "",
    responseProfileId: rule.responseProfileId || responseProfiles[0]?.id || "",
  };
}

function createOption(entry: CurtailmentSource | ResponseProfile): SelectOption {
  return {
    value: entry.id,
    label: entry.name,
  };
}

function validateAutomationFormValues(values: AutomationRuleFormValues): AutomationFormErrors {
  const errors: AutomationFormErrors = {};

  if (values.name.trim() === "") {
    errors.name = "Enter a rule name.";
  }
  if (values.sourceId === "") {
    errors.sourceId = "Select a trigger.";
  }
  if (values.responseProfileId === "") {
    errors.responseProfileId = "Select a response profile.";
  }

  return errors;
}

function InfoToggleContent({ ariaLabel, description, testId, triggerClassName }: InfoToggleContentProps): ReactElement {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef } = usePopover();
  const closeIgnoreSelectors = classNameToSelectors(triggerClassName);

  return (
    <div ref={triggerRef} className={`${triggerClassName} relative`}>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        ariaHasPopup
        ariaExpanded={isOpen}
        ariaLabel={ariaLabel}
        prefixIcon={<Info width={iconSizes.small} className="text-text-primary-70" />}
        onClick={() => setIsOpen((current) => !current)}
      />
      {isOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.normal}
          offset={8}
          className="!space-y-0"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={closeIgnoreSelectors}
          testId={testId}
        >
          <p className="text-300 text-text-primary-70">{description}</p>
        </Popover>
      ) : null}
    </div>
  );
}

function AutomationsInfoToggle(): ReactElement {
  return (
    <PopoverProvider>
      <InfoToggleContent
        ariaLabel="About automations"
        description={AUTOMATIONS_DESCRIPTION}
        testId="curtailment-automations-info-popover"
        triggerClassName="curtailment-automations-info-trigger"
      />
    </PopoverProvider>
  );
}

function SectionHeader({ title, buttonText, onButtonClick, infoToggle }: SectionHeaderProps): ReactElement {
  return (
    <div className="mb-4 flex items-center justify-between gap-4">
      <div className="flex min-w-0 items-center gap-4">
        <h2 className="truncate text-heading-200 font-medium text-text-primary">{title}</h2>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {infoToggle}
        <Button variant={variants.secondary} size={sizes.compact} text={buttonText} onClick={onButtonClick} />
      </div>
    </div>
  );
}

function AutomationsEmptyState(): ReactElement {
  return (
    <SettingsEmptyState
      size="section"
      title="No automations configured"
      description="Add an automation to trigger a response profile."
    />
  );
}

function AutomationsLoadingState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full items-center justify-center py-14">
      <ProgressCircular indeterminate />
    </div>
  );
}

function AutomationsErrorState({ message }: { message: string }): ReactElement {
  return <SettingsEmptyState size="section" title="Unable to load automations" description={message} />;
}

function AutomationDependencyMessage({ children }: { children: string }): ReactElement {
  return (
    <p className="mt-2 text-200 text-text-primary-70">
      <span className="mr-1 inline-block h-1 w-2.5 rounded-full bg-core-primary-20 align-middle" />
      {children}
    </p>
  );
}

function AutomationModal({
  open,
  mode,
  initialValues,
  sources,
  responseProfiles,
  isLoadingSources = false,
  loadSourcesError = null,
  isLoadingResponseProfiles = false,
  loadResponseProfilesError = null,
  saving = false,
  deleting = false,
  onDismiss,
  onSave,
  onDelete,
}: AutomationModalProps): ReactElement {
  const [values, setValues] = useState<AutomationRuleFormValues>(() => initialValues);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [showValidationErrors, setShowValidationErrors] = useState(false);
  const sourceOptions = useMemo(() => sources.map(createOption), [sources]);
  const responseProfileOptions = useMemo(() => responseProfiles.map(createOption), [responseProfiles]);
  const isBusy = saving || deleting;
  const validationErrors = useMemo(() => validateAutomationFormValues(values), [values]);
  const visibleValidationErrors = showValidationErrors ? validationErrors : {};
  const hasValidationErrors = Object.keys(validationErrors).length > 0;
  const isSaveUnavailable = isLoadingSources || isLoadingResponseProfiles || isBusy;

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- dependency options can load after the modal opens
    setValues((currentValues) => ({
      ...currentValues,
      sourceId: currentValues.sourceId || sources[0]?.id || "",
      responseProfileId: currentValues.responseProfileId || responseProfiles[0]?.id || "",
    }));
  }, [responseProfiles, sources]);

  const handleNameChange = useCallback((value: string) => {
    setValues((currentValues) => ({ ...currentValues, name: value }));
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
      setSaveError(error instanceof Error && error.message ? error.message : "Failed to save automation.");
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
      setSaveError(error instanceof Error && error.message ? error.message : "Failed to delete automation.");
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

  return (
    <Modal
      open={open}
      title={mode === "edit" ? "Edit automation" : "Create automation"}
      description={AUTOMATIONS_DESCRIPTION}
      onDismiss={onDismiss}
      size={modalSizes.standard}
      divider={false}
      testId="curtailment-automation-modal"
      buttons={modalButtons}
      bodyClassName="text-text-primary"
    >
      <div className="grid gap-6 pb-2" onKeyDown={handleFormKeyDown}>
        {saveError ? (
          <div className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical">{saveError}</div>
        ) : null}
        <Input
          id="automation-rule-name"
          label="Rule name"
          initValue={values.name}
          error={visibleValidationErrors.name}
          onChange={handleNameChange}
        />

        <div className="grid gap-3">
          <div className="text-200 font-semibold tracking-[0.08em] text-text-primary-50 uppercase">When</div>
          <div>
            <Select
              id="automation-trigger-source"
              label="Trigger"
              options={sourceOptions}
              value={values.sourceId}
              onChange={(sourceId) => setValues((currentValues) => ({ ...currentValues, sourceId }))}
              disabled={isLoadingSources || sourceOptions.length === 0}
              error={visibleValidationErrors.sourceId}
              testId="automation-trigger-source-select"
              forceBelow
            />
            {loadSourcesError ? (
              <AutomationDependencyMessage>Unable to load sources.</AutomationDependencyMessage>
            ) : null}
            {!isLoadingSources && !loadSourcesError && sourceOptions.length === 0 ? (
              <AutomationDependencyMessage>No sources available.</AutomationDependencyMessage>
            ) : null}
          </div>
        </div>

        <div className="grid gap-3">
          <div className="text-200 font-semibold tracking-[0.08em] text-text-primary-50 uppercase">Condition</div>
          <div>
            <Input
              id="automation-grid-signal"
              label="Grid signal"
              type="number"
              inputMode="numeric"
              initValue={GRID_SIGNAL_VALUE}
              readOnly
            />
            <p className="mt-2 text-300 text-text-primary-70">{GRID_SIGNAL_HINT}</p>
          </div>
        </div>

        <div className="grid gap-3">
          <div className="text-200 font-semibold tracking-[0.08em] text-text-primary-50 uppercase">Then</div>
          <div>
            <Select
              id="automation-response-profile"
              label="Response profile"
              options={responseProfileOptions}
              value={values.responseProfileId}
              onChange={(responseProfileId) => setValues((currentValues) => ({ ...currentValues, responseProfileId }))}
              disabled={isLoadingResponseProfiles || responseProfileOptions.length === 0}
              error={visibleValidationErrors.responseProfileId}
              testId="automation-response-profile-select"
              forceBelow
            />
            {loadResponseProfilesError ? (
              <AutomationDependencyMessage>Unable to load response profiles.</AutomationDependencyMessage>
            ) : null}
            {!isLoadingResponseProfiles && !loadResponseProfilesError && responseProfileOptions.length === 0 ? (
              <AutomationDependencyMessage>No response profiles available.</AutomationDependencyMessage>
            ) : null}
          </div>
        </div>
      </div>
    </Modal>
  );
}

function createAutomationColConfig(
  onToggle: (ruleId: string) => void,
  updatingRuleIds: ReadonlySet<string>,
): ColConfig<AutomationRuleWithDetails, string, AutomationColumn> {
  return {
    [automationCols.name]: {
      component: (rule) => (
        <span className="block max-w-full truncate text-emphasis-300 text-text-primary">{rule.name}</span>
      ),
      width: "w-[31%] phone:w-auto",
    },
    [automationCols.condition]: {
      component: (rule) => <span className="truncate text-text-primary">{rule.conditionSummary}</span>,
      width: "w-[39%] phone:w-auto",
    },
    [automationCols.responseProfile]: {
      component: (rule) => <span className="truncate text-text-primary">{rule.responseProfileName}</span>,
      width: "w-[24%] phone:w-auto",
    },
    [automationCols.enabled]: {
      component: (rule) => (
        <div className="flex justify-end" data-interactive>
          <Switch checked={rule.enabled} setChecked={() => onToggle(rule.id)} disabled={updatingRuleIds.has(rule.id)} />
        </div>
      ),
      width: "w-[6%] phone:w-14",
    },
  };
}

function mapAutomationRules(
  automationRules: AutomationRule[],
  responseProfiles: ResponseProfile[],
): AutomationRuleWithDetails[] {
  const responseProfileNamesById = new Map(responseProfiles.map((profile) => [profile.id, profile.name]));

  return automationRules.map((rule) => ({
    ...rule,
    responseProfileName:
      responseProfileNamesById.get(rule.responseProfileId) ?? rule.responseProfileName ?? "Unknown profile",
  }));
}

function getAutomationConditionSummary(sourceName: string): string {
  return sourceName ? `${sourceName} grid signal changes to 0` : "Grid signal changes to 0";
}

function getAutomationsEmptyState(loadError: string | null, isLoading: boolean): ReactElement {
  if (loadError) {
    return <AutomationsErrorState message={loadError} />;
  }

  if (isLoading) {
    return <AutomationsLoadingState />;
  }

  return <AutomationsEmptyState />;
}

export function CurtailmentAutomationsContent({
  initialAutomationRules = emptyAutomations,
  automationRules: controlledAutomationRules,
  sources = emptySources,
  responseProfiles = emptyResponseProfiles,
  isLoading = false,
  loadError = null,
  isCreating = false,
  updatingRuleIds = new Set<string>(),
  isLoadingSources = false,
  loadSourcesError = null,
  isLoadingResponseProfiles = false,
  loadResponseProfilesError = null,
  onCreateAutomation,
  onUpdateAutomation,
  onToggleAutomation,
  onDeleteAutomation,
}: CurtailmentAutomationsContentProps): ReactElement {
  const [localAutomationRules, setLocalAutomationRules] = useState<AutomationRule[]>(() => [...initialAutomationRules]);
  const [isAutomationModalOpen, setIsAutomationModalOpen] = useState(false);
  const [editingAutomationRule, setEditingAutomationRule] = useState<AutomationRule | null>(null);
  const automationRules = controlledAutomationRules ?? localAutomationRules;

  const rulesWithDetails = useMemo(
    () => mapAutomationRules(automationRules, responseProfiles),
    [automationRules, responseProfiles],
  );
  const automationModalMode = editingAutomationRule ? "edit" : "create";
  const automationModalInitialValues = useMemo(
    () => getAutomationFormValuesFromRule(editingAutomationRule, sources, responseProfiles),
    [editingAutomationRule, responseProfiles, sources],
  );

  const openCreateAutomationModal = useCallback(() => {
    setEditingAutomationRule(null);
    setIsAutomationModalOpen(true);
  }, []);

  const openEditAutomationModal = useCallback((rule: AutomationRule) => {
    setEditingAutomationRule(rule);
    setIsAutomationModalOpen(true);
  }, []);

  const closeAutomationModal = useCallback(() => {
    setIsAutomationModalOpen(false);
    setEditingAutomationRule(null);
  }, []);

  const toggleAutomation = useCallback(
    (ruleId: string) => {
      const rule = automationRules.find((currentRule) => currentRule.id === ruleId);
      if (!rule || updatingRuleIds.has(ruleId)) {
        return;
      }

      const nextEnabled = !rule.enabled;
      if (onToggleAutomation) {
        void onToggleAutomation(rule, nextEnabled).catch(() => {});
        return;
      }

      setLocalAutomationRules((currentRules) =>
        currentRules.map((currentRule) =>
          currentRule.id === ruleId ? { ...currentRule, enabled: nextEnabled } : currentRule,
        ),
      );
    },
    [automationRules, onToggleAutomation, updatingRuleIds],
  );

  const automationColConfig = useMemo(
    () => createAutomationColConfig(toggleAutomation, updatingRuleIds),
    [toggleAutomation, updatingRuleIds],
  );

  const handleCreateAutomation = useCallback(
    async (values: AutomationRuleFormValues) => {
      const createdRule = await onCreateAutomation?.(values);
      if (controlledAutomationRules) {
        return;
      }

      if (createdRule) {
        setLocalAutomationRules((currentRules) => [
          ...currentRules.filter((currentRule) => currentRule.id !== createdRule.id),
          createdRule,
        ]);
        return;
      }

      const source = sources.find((currentSource) => currentSource.id === values.sourceId);
      const nextRule: AutomationRule = {
        id: createAutomationRuleId(values.name, automationRules),
        priority: automationRules.length + 1,
        name: values.name.trim(),
        conditionType: "mqttTriggerTargetOff",
        conditionSummary: getAutomationConditionSummary(source?.name ?? ""),
        sourceId: values.sourceId,
        responseProfileId: values.responseProfileId,
        enabled: true,
      };

      setLocalAutomationRules((currentRules) => [...currentRules, nextRule]);
    },
    [automationRules, controlledAutomationRules, onCreateAutomation, sources],
  );

  const handleSaveAutomation = useCallback(
    async (values: AutomationRuleFormValues) => {
      if (!editingAutomationRule) {
        await handleCreateAutomation(values);
        return;
      }

      const updatedRule = await onUpdateAutomation?.(editingAutomationRule, values);
      if (controlledAutomationRules) {
        return;
      }

      if (updatedRule) {
        setLocalAutomationRules((currentRules) =>
          currentRules.map((currentRule) => (currentRule.id === updatedRule.id ? updatedRule : currentRule)),
        );
        return;
      }

      const source = sources.find((currentSource) => currentSource.id === values.sourceId);
      const conditionSummary =
        values.sourceId === editingAutomationRule.sourceId
          ? editingAutomationRule.conditionSummary
          : getAutomationConditionSummary(source?.name ?? "");
      const nextRule: AutomationRule = {
        ...editingAutomationRule,
        name: values.name.trim(),
        conditionSummary,
        sourceId: values.sourceId,
        responseProfileId: values.responseProfileId,
      };

      setLocalAutomationRules((currentRules) =>
        currentRules.map((currentRule) => (currentRule.id === nextRule.id ? nextRule : currentRule)),
      );
    },
    [controlledAutomationRules, editingAutomationRule, handleCreateAutomation, onUpdateAutomation, sources],
  );

  const handleDeleteAutomation = useCallback(async () => {
    if (!editingAutomationRule) {
      return;
    }

    await onDeleteAutomation?.(editingAutomationRule);
    if (!controlledAutomationRules) {
      setLocalAutomationRules((currentRules) => currentRules.filter((rule) => rule.id !== editingAutomationRule.id));
    }
  }, [controlledAutomationRules, editingAutomationRule, onDeleteAutomation]);

  const isEditingAutomation = editingAutomationRule ? updatingRuleIds.has(editingAutomationRule.id) : false;
  const automationsNoDataElement = getAutomationsEmptyState(loadError, isLoading);

  return (
    <section
      className={clsx(
        "curtailment-settings__section curtailment-settings__section--last pb-10",
        automationRules.length === 0 && "min-h-[300px]",
      )}
    >
      <SectionHeader
        title="Automations"
        buttonText="Create automation"
        onButtonClick={openCreateAutomationModal}
        infoToggle={<AutomationsInfoToggle />}
      />
      <List<AutomationRuleWithDetails, string, AutomationColumn>
        activeCols={activeAutomationCols}
        colTitles={automationColTitles}
        columnHeaderAriaLabels={automationColumnAriaLabels}
        colConfig={automationColConfig}
        items={rulesWithDetails}
        itemKey="id"
        total={rulesWithDetails.length}
        hideTotal
        itemName={{ singular: "automation", plural: "automations" }}
        stickyFirstColumn={false}
        isRowDisabled={(rule) => !rule.enabled}
        columnsExemptFromDisabledStyling={automationColumnsExemptFromDisabledStyling}
        tableClassName={automationTableClassName}
        noDataElement={automationsNoDataElement}
        applyColumnWidthsToCells
        onRowClick={openEditAutomationModal}
      />

      <AutomationModal
        key={
          isAutomationModalOpen
            ? `automation-modal-${automationModalMode}-${editingAutomationRule?.id ?? "new"}`
            : "automation-modal-closed"
        }
        open={isAutomationModalOpen}
        mode={automationModalMode}
        initialValues={automationModalInitialValues}
        sources={sources}
        responseProfiles={responseProfiles}
        isLoadingSources={isLoadingSources}
        loadSourcesError={loadSourcesError}
        isLoadingResponseProfiles={isLoadingResponseProfiles}
        loadResponseProfilesError={loadResponseProfilesError}
        saving={editingAutomationRule ? isEditingAutomation : isCreating}
        deleting={isEditingAutomation}
        onDismiss={closeAutomationModal}
        onSave={handleSaveAutomation}
        onDelete={editingAutomationRule ? handleDeleteAutomation : undefined}
      />
    </section>
  );
}

export default CurtailmentAutomationsContent;
