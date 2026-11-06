import { type ReactElement, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import type {
  AutomationRule,
  AutomationRuleFormValues,
  CurtailmentSource,
  ResponseProfile,
} from "@/protoFleet/api/curtailmentTypes";
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
  SectionDependencyMessage,
  SectionHeader,
} from "@/protoFleet/features/settings/components/Curtailment/CurtailmentSection";
import { useAutomationRuleModalActions } from "@/protoFleet/features/settings/components/Curtailment/useAutomationRuleModalActions";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";
import Select, { type SelectOption } from "@/shared/components/Select";

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

const emptyAutomations: AutomationRule[] = [];
const emptySources: CurtailmentSource[] = [];
const emptyResponseProfiles: ResponseProfile[] = [];

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
  onToggleAutomation?: (
    rule: AutomationRule,
    enabled: boolean,
    expectedResponseProfileRevision?: string,
  ) => Promise<AutomationRule | void>;
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
  const responseProfile = responseProfiles[0];
  return {
    name: "",
    sourceId: sources[0]?.id ?? "",
    responseProfileId: responseProfile?.id ?? "",
    responseProfileRevision: responseProfile?.revision ?? "",
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

  const responseProfile = responseProfiles.find((profile) => profile.id === rule.responseProfileId);
  return {
    name: rule.name,
    sourceId: rule.sourceId ?? sources[0]?.id ?? "",
    responseProfileId: rule.responseProfileId,
    responseProfileRevision: responseProfile?.revision ?? "",
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
  } else if (values.responseProfileRevision === "") {
    errors.responseProfileId = "Reload response profiles before saving.";
  }

  return errors;
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
  const [showValidationErrors, setShowValidationErrors] = useState(false);
  const sourceOptions = useMemo(() => sources.map(createOption), [sources]);
  const isBusy = saving || deleting;
  const validationErrors = useMemo(() => validateAutomationFormValues(values), [values]);
  const visibleValidationErrors = showValidationErrors ? validationErrors : {};
  const hasValidationErrors = Object.keys(validationErrors).length > 0;
  const isSelectedResponseProfileUnavailable =
    values.responseProfileId !== "" &&
    !responseProfiles.some((profile) => profile.id === values.responseProfileId && profile.revision);
  const isSaveUnavailable =
    isLoadingSources || isLoadingResponseProfiles || isSelectedResponseProfileUnavailable || isBusy;

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- dependency options can load after the modal opens
    setValues((currentValues) => {
      const responseProfileId = currentValues.responseProfileId || responseProfiles[0]?.id || "";
      const responseProfile = responseProfiles.find((profile) => profile.id === responseProfileId);
      return {
        ...currentValues,
        sourceId: currentValues.sourceId || sources[0]?.id || "",
        responseProfileId,
        responseProfileRevision:
          currentValues.responseProfileId === responseProfileId && currentValues.responseProfileRevision
            ? currentValues.responseProfileRevision
            : (responseProfile?.revision ?? ""),
      };
    });
  }, [responseProfiles, sources]);

  const handleNameChange = useCallback((value: string) => {
    setValues((currentValues) => ({ ...currentValues, name: value }));
  }, []);

  const { saveError, handleFormKeyDown, modalButtons } = useAutomationRuleModalActions({
    mode,
    ruleNoun: "automation",
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
        <AutomationRuleSaveError message={saveError} />
        <Input
          id="automation-rule-name"
          label="Rule name"
          initValue={values.name}
          error={visibleValidationErrors.name}
          onChange={handleNameChange}
        />

        <div className="grid gap-3">
          <div className={automationRuleModalSectionLabelClassName}>When</div>
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
            {loadSourcesError ? <SectionDependencyMessage>Unable to load sources.</SectionDependencyMessage> : null}
            {!isLoadingSources && !loadSourcesError && sourceOptions.length === 0 ? (
              <SectionDependencyMessage>No sources available.</SectionDependencyMessage>
            ) : null}
          </div>
        </div>

        <div className="grid gap-3">
          <div className={automationRuleModalSectionLabelClassName}>Condition</div>
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
          <div className={automationRuleModalSectionLabelClassName}>Then</div>
          <AutomationRuleResponseProfileField
            id="automation-response-profile"
            testId="automation-response-profile-select"
            responseProfiles={responseProfiles}
            value={values.responseProfileId}
            onChange={(responseProfileId, responseProfileRevision) =>
              setValues((currentValues) => ({ ...currentValues, responseProfileId, responseProfileRevision }))
            }
            error={visibleValidationErrors.responseProfileId}
            isLoading={isLoadingResponseProfiles}
            loadError={loadResponseProfilesError}
          />
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
    [automationCols.name]: automationRuleNameColumn("automation", "w-[31%] phone:w-auto"),
    [automationCols.condition]: {
      component: (rule) => <span className="truncate text-text-primary">{rule.conditionSummary}</span>,
      width: "w-[39%] phone:w-auto",
    },
    [automationCols.responseProfile]: {
      component: (rule) => <span className="truncate text-text-primary">{rule.responseProfileName}</span>,
      width: "w-[24%] phone:w-auto",
    },
    [automationCols.enabled]: automationRuleEnabledColumn(onToggle, updatingRuleIds),
  };
}

function getAutomationConditionSummary(sourceName: string): string {
  return sourceName ? `${sourceName} grid signal changes to 0` : "Grid signal changes to 0";
}

export function CurtailmentAutomationsContent({
  initialAutomationRules = emptyAutomations,
  automationRules: controlledAutomationRules,
  sources = emptySources,
  responseProfiles = emptyResponseProfiles,
  isLoading = false,
  loadError = null,
  isCreating = false,
  updatingRuleIds = emptyUpdatingRuleIds,
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
  const automationReadyResponseProfiles = useMemo(
    () => responseProfiles.filter((profile) => profile.isAutomationReady && profile.revision),
    [responseProfiles],
  );
  const automationReadyResponseProfileIds = useMemo(
    () => new Set(automationReadyResponseProfiles.map((profile) => profile.id)),
    [automationReadyResponseProfiles],
  );

  const rulesWithDetails = useMemo(
    () => withResponseProfileDetails(automationRules, responseProfiles),
    [automationRules, responseProfiles],
  );
  const automationModalMode = editingAutomationRule ? "edit" : "create";
  const automationModalInitialValues = useMemo(
    () => getAutomationFormValuesFromRule(editingAutomationRule, sources, automationReadyResponseProfiles),
    [automationReadyResponseProfiles, editingAutomationRule, sources],
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
      if (
        !rule ||
        updatingRuleIds.has(ruleId) ||
        (!rule.enabled && !automationReadyResponseProfileIds.has(rule.responseProfileId))
      ) {
        return;
      }

      const nextEnabled = !rule.enabled;
      if (onToggleAutomation) {
        const expectedResponseProfileRevision = nextEnabled
          ? automationReadyResponseProfiles.find((profile) => profile.id === rule.responseProfileId)?.revision
          : undefined;
        void onToggleAutomation(rule, nextEnabled, expectedResponseProfileRevision).catch(() => {});
        return;
      }

      setLocalAutomationRules((currentRules) =>
        currentRules.map((currentRule) =>
          currentRule.id === ruleId ? { ...currentRule, enabled: nextEnabled } : currentRule,
        ),
      );
    },
    [
      automationReadyResponseProfileIds,
      automationReadyResponseProfiles,
      automationRules,
      onToggleAutomation,
      updatingRuleIds,
    ],
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
        isRowDisabled={(rule) => !rule.enabled || !rule.isResponseProfileAutomationReady}
        columnsExemptFromDisabledStyling={automationColumnsExemptFromDisabledStyling}
        tableClassName={automationRuleTableClassName}
        noDataElement={automationRuleListEmptyState({
          loadError,
          isLoading,
          errorTitle: "Unable to load automations",
          emptyTitle: "No automations configured",
          emptyDescription: "Add an automation to trigger a response profile.",
        })}
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
        responseProfiles={automationReadyResponseProfiles}
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
