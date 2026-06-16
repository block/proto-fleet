import { type KeyboardEvent, type ReactElement, useCallback, useEffect, useMemo, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import clsx from "clsx";

import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import useCurtailmentAutomationRules from "@/protoFleet/api/useCurtailmentAutomationRules";
import useCurtailmentResponseProfiles, {
  getResponseProfileScopeLabelForActionType,
} from "@/protoFleet/api/useCurtailmentResponseProfiles";
import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentSubmitValues,
  type ResponseProfileModalMode,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentAutomationsContent from "@/protoFleet/features/settings/components/Curtailment/CurtailmentAutomations";
import { isInputEnterSaveEvent } from "@/protoFleet/features/settings/components/Curtailment/keyboard";
import type {
  AutomationRule,
  AutomationRuleFormValues,
  CurtailmentHealth,
  CurtailmentSource,
  CurtailmentSourceFormValues,
  ResponseProfile,
  ResponseProfileFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useHasPermission } from "@/protoFleet/store";
import { Alert, Info, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import Card, { cardType } from "@/shared/components/Card";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import type { SelectOption } from "@/shared/components/Select";
import Switch from "@/shared/components/Switch";
import { positions } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { classNameToSelectors } from "@/shared/utils/cssUtils";
import "./CurtailmentSettingsPage.css";

const CURTAILMENT_PAGE_DESCRIPTION =
  "Configure response profiles, manage external signal sources, and define automations that trigger curtailment.";
const RESPONSE_PROFILES_DESCRIPTION = "Saved configurations that define how much power to shed and how to restore it.";
const SOURCES_DESCRIPTION = "MaestroOS MQTT brokers that publish curtailment signals.";
const SOURCE_CONNECTION_FAILURE_MESSAGE =
  "We couldn't connect with your source. Review your source details and try again.";
const MAX_BROKER_PORT = 65_535;

const responseProfileSelectionStrategyOptions: SelectOption[] = [
  { value: "leastEfficientFirst", label: "Least efficient first" },
];

const responseProfileRestoreBehaviorOptions: SelectOption[] = [
  { value: "automaticBatchRestore", label: "Restore in batches" },
  { value: "automaticImmediateRestore", label: "Restore immediately" },
];

const responseProfileSelectionStrategyLabel = Object.fromEntries(
  responseProfileSelectionStrategyOptions.map((option) => [option.value, option.label]),
) as Record<ResponseProfileFormValues["selectionStrategy"], string>;

const responseProfileRestoreBehaviorLabel = Object.fromEntries(
  responseProfileRestoreBehaviorOptions.map((option) => [option.value, option.label]),
) as Record<ResponseProfileFormValues["restoreBehavior"], string>;

const curtailmentSourceCols = {
  name: "name",
  lastSignalValue: "lastSignalValue",
  lastSignalUpdate: "lastSignalUpdate",
  health: "health",
  enabled: "enabled",
} as const;

type CurtailmentSourceColumn = (typeof curtailmentSourceCols)[keyof typeof curtailmentSourceCols];

const activeCurtailmentSourceCols: CurtailmentSourceColumn[] = [
  curtailmentSourceCols.name,
  curtailmentSourceCols.lastSignalValue,
  curtailmentSourceCols.lastSignalUpdate,
  curtailmentSourceCols.health,
  curtailmentSourceCols.enabled,
];

const curtailmentSourceColTitles: ColTitles<CurtailmentSourceColumn> = {
  name: "Name",
  lastSignalValue: "Last signal",
  lastSignalUpdate: "Updated",
  health: "Connection",
  enabled: "",
};

const curtailmentSourceColumnAriaLabels: Partial<Record<CurtailmentSourceColumn, string>> = {
  enabled: "Enabled",
};

const curtailmentSourceColumnsExemptFromDisabledStyling = new Set<CurtailmentSourceColumn>([
  curtailmentSourceCols.enabled,
]);

const curtailmentSourcesTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "[&_thead_th]:text-text-primary-50",
  "phone:[&_thead_th:last-child]:w-9",
  "phone:[&_thead_th:last-child>div]:w-9",
].join(" ");

const sourceHealthDotClassName: Record<CurtailmentHealth, string> = {
  connected: "bg-intent-success-fill",
  waitingForSignal: "bg-intent-warning-fill",
  noSignal: "bg-intent-critical-fill",
  offline: "bg-intent-critical-fill",
};

const emptySourceFormValues: CurtailmentSourceFormValues = {
  name: "",
  brokerPrimaryHost: "",
  brokerSecondaryHost: "",
  brokerPort: "",
  topic: "",
  username: "",
  password: "",
};

const emptyCurtailmentSources: CurtailmentSource[] = [];
const emptyResponseProfiles: ResponseProfile[] = [];
const emptyAutomationRules: AutomationRule[] = [];
const emptyUpdatingSourceIds = new Set<string>();
const emptyUpdatingResponseProfileIds = new Set<string>();
const emptyUpdatingAutomationRuleIds = new Set<string>();
const savedPasswordPlaceholder = "......";
const immediateRestoreBatchSize = "10000";

type SourceModalMode = "create" | "edit";

const emptyResponseProfileFormValues: ResponseProfileFormValues = {
  name: "",
  actionType: "fullFleet",
  targetKw: "",
  deviceIdentifiers: [],
  siteId: "",
  siteName: "",
  selectionStrategy: "leastEfficientFirst",
  restoreBehavior: "automaticBatchRestore",
  minDurationSec: "",
  maxDurationSec: "900",
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  responseDeadlineMinutes: "15",
  includeMaintenance: true,
};

const sourceInputIds = {
  name: "source-name",
  brokerPrimaryHost: "source-host-primary",
  brokerSecondaryHost: "source-host-backup",
  brokerPort: "source-port",
  topic: "source-topic",
  username: "source-username",
  password: "source-password",
} as const;

const sourceInputIdToFormKey: Record<string, keyof CurtailmentSourceFormValues> = {
  [sourceInputIds.name]: "name",
  [sourceInputIds.brokerPrimaryHost]: "brokerPrimaryHost",
  [sourceInputIds.brokerSecondaryHost]: "brokerSecondaryHost",
  [sourceInputIds.brokerPort]: "brokerPort",
  [sourceInputIds.topic]: "topic",
  [sourceInputIds.username]: "username",
  [sourceInputIds.password]: "password",
};

function isPositiveInteger(value: string): boolean {
  return /^[1-9]\d*$/.test(value.trim());
}

function getOptionValueByLabel<TValue extends string>(
  options: SelectOption[],
  label: string,
  fallbackValue: TValue,
): TValue {
  return (options.find((option) => option.label === label)?.value ?? fallbackValue) as TValue;
}

function getResponseProfileTargetSummary(values: ResponseProfileFormValues): string {
  const targetKw = Number(values.targetKw).toLocaleString();

  switch (values.actionType) {
    case "fixedKwReduction":
      return `${targetKw} kW target`;
    case "fullFleet":
    default:
      return "100% reduction";
  }
}

function getResponseProfileDeadlineSummary(values: ResponseProfileFormValues): string {
  const minutes = Number(values.responseDeadlineMinutes);
  return minutes === 1 ? "Within 1 min" : `Within ${minutes} min`;
}

function getResponseProfileScopeSummary(values: ResponseProfileFormValues): string {
  return values.siteId
    ? values.siteName || `Site ${values.siteId}`
    : getResponseProfileScopeLabelForActionType(values.actionType);
}

function secondsToDeadlineMinutes(value: string): string {
  const seconds = Number(value);

  if (!Number.isFinite(seconds) || seconds <= 0) {
    return emptyResponseProfileFormValues.responseDeadlineMinutes;
  }

  return Math.max(Math.ceil(seconds / 60), 1).toString();
}

function minutesToSeconds(value: string): string {
  const minutes = Number(value);

  if (!Number.isFinite(minutes) || minutes <= 0) {
    return emptyResponseProfileFormValues.maxDurationSec;
  }

  return String(minutes * 60);
}

function createResponseProfileId(name: string, existingProfiles: ResponseProfile[]): string {
  const baseSlug =
    name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "response-profile";
  const existingIds = new Set(existingProfiles.map((profile) => profile.id));

  let candidate = baseSlug;
  let suffix = 2;
  while (existingIds.has(candidate)) {
    candidate = `${baseSlug}-${suffix}`;
    suffix += 1;
  }

  return candidate;
}

function createResponseProfileFromFormValues(
  values: ResponseProfileFormValues,
  existingProfiles: ResponseProfile[],
  existingProfile?: ResponseProfile,
): ResponseProfile {
  const normalizedValues: ResponseProfileFormValues = {
    ...values,
    name: values.name.trim(),
    targetKw: values.targetKw.trim(),
    deviceIdentifiers: [],
    siteId: values.siteId.trim(),
    siteName: values.siteId.trim() ? values.siteName.trim() : "",
    minDurationSec: values.minDurationSec.trim(),
    maxDurationSec: values.maxDurationSec.trim(),
    curtailBatchSize: values.curtailBatchSize.trim(),
    curtailBatchIntervalSec: values.curtailBatchIntervalSec.trim(),
    restoreBatchSize: values.restoreBatchSize.trim(),
    restoreIntervalSec: values.restoreIntervalSec.trim(),
    responseDeadlineMinutes: values.responseDeadlineMinutes.trim(),
  };

  return {
    id: existingProfile?.id ?? createResponseProfileId(normalizedValues.name, existingProfiles),
    name: normalizedValues.name,
    targetSummary: getResponseProfileTargetSummary(normalizedValues),
    scope: getResponseProfileScopeSummary(normalizedValues),
    selectionStrategy: responseProfileSelectionStrategyLabel[normalizedValues.selectionStrategy],
    restoreBehavior: responseProfileRestoreBehaviorLabel[normalizedValues.restoreBehavior],
    deadlineSummary: getResponseProfileDeadlineSummary(normalizedValues),
    formValues: normalizedValues,
  };
}

function removeResponseProfileScope(values: ResponseProfileFormValues): ResponseProfileFormValues {
  const siteId = values.siteId.trim();

  return {
    ...values,
    deviceIdentifiers: [],
    siteId,
    siteName: siteId ? values.siteName.trim() : "",
  };
}

function createResponseProfileFormValuesFromProfile(profile: ResponseProfile): ResponseProfileFormValues {
  if (profile.formValues) {
    return removeResponseProfileScope(profile.formValues);
  }

  const targetKwMatch = profile.targetSummary.match(/(\d+(?:\.\d+)?)/);
  const actionType = targetKwMatch ? "fixedKwReduction" : "fullFleet";

  return {
    name: profile.name,
    actionType,
    targetKw: targetKwMatch?.[1] ?? "",
    deviceIdentifiers: [],
    siteId: "",
    siteName: "",
    selectionStrategy: getOptionValueByLabel(
      responseProfileSelectionStrategyOptions,
      profile.selectionStrategy,
      emptyResponseProfileFormValues.selectionStrategy,
    ),
    restoreBehavior: getOptionValueByLabel(
      responseProfileRestoreBehaviorOptions,
      profile.restoreBehavior,
      emptyResponseProfileFormValues.restoreBehavior,
    ),
    minDurationSec: emptyResponseProfileFormValues.minDurationSec,
    maxDurationSec: minutesToSeconds(
      profile.deadlineSummary.match(/(\d+)/)?.[1] ?? emptyResponseProfileFormValues.responseDeadlineMinutes,
    ),
    curtailBatchSize: emptyResponseProfileFormValues.curtailBatchSize,
    curtailBatchIntervalSec: emptyResponseProfileFormValues.curtailBatchIntervalSec,
    restoreBatchSize:
      profile.restoreBehavior === responseProfileRestoreBehaviorLabel.automaticImmediateRestore
        ? immediateRestoreBatchSize
        : emptyResponseProfileFormValues.restoreBatchSize,
    restoreIntervalSec: emptyResponseProfileFormValues.restoreIntervalSec,
    responseDeadlineMinutes:
      profile.deadlineSummary.match(/(\d+)/)?.[1] ?? emptyResponseProfileFormValues.responseDeadlineMinutes,
    includeMaintenance: emptyResponseProfileFormValues.includeMaintenance,
  };
}

function createCurtailmentFormValuesFromResponseProfile(
  values: ResponseProfileFormValues,
): Partial<CurtailmentFormValues> {
  const restoreBatchSize =
    values.restoreBatchSize ||
    (values.restoreBehavior === "automaticImmediateRestore" ? immediateRestoreBatchSize : "");
  const siteId = values.siteId.trim();
  const siteName = siteId ? values.siteName || `Site ${siteId}` : "";

  return {
    scopeType: siteId ? "site" : "wholeOrg",
    scopeId: siteId ? siteName : "whole-org",
    siteId,
    deviceSetIds: [],
    deviceIdentifiers: [],
    responseProfileId: "customPlan",
    curtailmentMode: values.actionType,
    minerSelectionStrategy: values.selectionStrategy,
    targetKw: values.targetKw,
    minDurationSec: values.minDurationSec,
    maxDurationSec: values.maxDurationSec || minutesToSeconds(values.responseDeadlineMinutes),
    curtailBatchSize: values.curtailBatchSize,
    curtailBatchIntervalSec: values.curtailBatchIntervalSec,
    restoreBatchSize,
    restoreIntervalSec: values.restoreIntervalSec,
    reason: values.name,
    includeMaintenance: values.includeMaintenance,
  };
}

function getResponseProfileRestoreBehavior(
  values: CurtailmentSubmitValues,
): ResponseProfileFormValues["restoreBehavior"] {
  const restoreBatchSize = Number(values.restoreBatchSize);
  const restoreIntervalSec = Number(values.restoreIntervalSec || "0");

  return Number.isFinite(restoreBatchSize) &&
    restoreBatchSize >= Number(immediateRestoreBatchSize) &&
    Number.isFinite(restoreIntervalSec) &&
    restoreIntervalSec === 0
    ? "automaticImmediateRestore"
    : "automaticBatchRestore";
}

function createResponseProfileFormValuesFromCurtailmentValues(
  values: CurtailmentSubmitValues,
): ResponseProfileFormValues {
  const siteId = values.scopeType === "site" ? (values.siteId ?? "") : "";
  const siteName = siteId ? (values.scopeId ?? "") : "";

  return {
    name: values.reason,
    actionType: values.curtailmentMode,
    targetKw: values.targetKw,
    deviceIdentifiers: [],
    siteId,
    siteName,
    selectionStrategy: values.minerSelectionStrategy,
    restoreBehavior: getResponseProfileRestoreBehavior(values),
    minDurationSec: values.minDurationSec,
    maxDurationSec: values.maxDurationSec,
    curtailBatchSize: values.curtailBatchSize,
    curtailBatchIntervalSec: values.curtailBatchIntervalSec,
    restoreBatchSize: values.restoreBatchSize,
    restoreIntervalSec: values.restoreIntervalSec,
    responseDeadlineMinutes: secondsToDeadlineMinutes(values.maxDurationSec),
    includeMaintenance: values.includeMaintenance,
  };
}

function createSourceFormValuesFromSource(source: CurtailmentSource): CurtailmentSourceFormValues {
  return {
    name: source.name,
    brokerPrimaryHost: source.brokerPrimaryHost ?? source.brokerHosts[0] ?? "",
    brokerSecondaryHost: source.brokerSecondaryHost ?? source.brokerHosts[1] ?? "",
    brokerPort: source.port.toString(),
    topic: source.topic,
    username: source.username,
    password: "",
  };
}

function applySourceFormValues(source: CurtailmentSource, values: CurtailmentSourceFormValues): CurtailmentSource {
  const brokerPrimaryHost = values.brokerPrimaryHost.trim();
  const brokerSecondaryHost = values.brokerSecondaryHost.trim();

  return {
    ...source,
    name: values.name.trim(),
    brokerHosts: [brokerPrimaryHost, brokerSecondaryHost].filter(Boolean),
    brokerPrimaryHost,
    brokerSecondaryHost,
    port: Number(values.brokerPort),
    topic: values.topic.trim(),
    username: values.username.trim(),
  };
}

function sourceCredentialFieldsChanged(
  values: CurtailmentSourceFormValues,
  initialValues: CurtailmentSourceFormValues,
): boolean {
  return (
    values.brokerPrimaryHost.trim() !== initialValues.brokerPrimaryHost.trim() ||
    values.brokerSecondaryHost.trim() !== initialValues.brokerSecondaryHost.trim() ||
    values.brokerPort.trim() !== initialValues.brokerPort.trim() ||
    values.username.trim() !== initialValues.username.trim()
  );
}

function validateSourceFormValues(values: CurtailmentSourceFormValues, passwordRequired: boolean): SourceFormErrors {
  const errors: SourceFormErrors = {};

  if (values.name.trim() === "") {
    errors.name = "Enter a configuration name.";
  }
  if (values.brokerPrimaryHost.trim() === "") {
    errors.brokerPrimaryHost = "Enter broker host 1.";
  }
  if (values.brokerSecondaryHost.trim() === "") {
    errors.brokerSecondaryHost = "Enter broker host 2.";
  }
  if (values.topic.trim() === "") {
    errors.topic = "Enter a topic.";
  }
  if (values.username.trim() === "") {
    errors.username = "Enter a username.";
  }
  if (passwordRequired && values.password === "") {
    errors.password = "Enter a password.";
  }
  if (values.brokerPort.trim() === "") {
    errors.brokerPort = "Enter a port.";
  } else if (!isPositiveInteger(values.brokerPort)) {
    errors.brokerPort = "Enter port as a whole number greater than 0.";
  } else if (Number(values.brokerPort) > MAX_BROKER_PORT) {
    errors.brokerPort = `Enter port of ${MAX_BROKER_PORT.toLocaleString()} or less.`;
  }

  return errors;
}

function getErrorMessage(error: unknown, fallbackMessage: string): string {
  return error instanceof Error && error.message ? error.message : fallbackMessage;
}

const sourceHealthLabel: Record<CurtailmentHealth, string> = {
  connected: "Connected",
  waitingForSignal: "Waiting for signal",
  noSignal: "No signal",
  offline: "Offline",
};

function formatSourceHealth(health: CurtailmentSource["health"]): string {
  return sourceHealthLabel[health];
}

type InfoToggleContentProps = {
  ariaLabel: string;
  description: string;
  testId: string;
  triggerClassName: string;
};

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

function ResponseProfilesInfoToggle(): ReactElement {
  return (
    <PopoverProvider>
      <InfoToggleContent
        ariaLabel="About response profiles"
        description={RESPONSE_PROFILES_DESCRIPTION}
        testId="curtailment-response-profiles-info-popover"
        triggerClassName="curtailment-response-profiles-info-trigger"
      />
    </PopoverProvider>
  );
}

function SourcesInfoToggle(): ReactElement {
  return (
    <PopoverProvider>
      <InfoToggleContent
        ariaLabel="About sources"
        description={SOURCES_DESCRIPTION}
        testId="curtailment-sources-info-popover"
        triggerClassName="curtailment-sources-info-trigger"
      />
    </PopoverProvider>
  );
}

function SourcesEmptyState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
      <div className="text-heading-200 text-text-primary">No sources configured</div>
      <p className="mt-1 text-400 text-text-primary-70">Add a MaestroOS MQTT source to receive curtailment signals.</p>
    </div>
  );
}

function SourcesLoadingState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full items-center justify-center py-14">
      <ProgressCircular indeterminate dataTestId="curtailment-sources-loading" />
    </div>
  );
}

type SourcesErrorStateProps = {
  message: string;
};

function SourcesErrorState({ message }: SourcesErrorStateProps): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
      <div className="text-heading-200 text-text-primary">Unable to load sources</div>
      <p className="mt-1 text-400 text-text-primary-70">{message}</p>
    </div>
  );
}

function ResponseProfilesEmptyState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
      <div className="text-heading-200 text-text-primary">No response profiles configured</div>
      <p className="mt-1 text-400 text-text-primary-70">
        Add a profile to reuse curtailment actions across automation rules.
      </p>
    </div>
  );
}

function ResponseProfilesLoadingState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full items-center justify-center py-14">
      <ProgressCircular indeterminate dataTestId="curtailment-response-profiles-loading" />
    </div>
  );
}

type ResponseProfilesErrorStateProps = {
  message: string;
};

function ResponseProfilesErrorState({ message }: ResponseProfilesErrorStateProps): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
      <div className="text-heading-200 text-text-primary">Unable to load response profiles</div>
      <p className="mt-1 text-400 text-text-primary-70">{message}</p>
    </div>
  );
}

type ResponseProfileCardProps = {
  profile: ResponseProfile;
  onEdit: (profile: ResponseProfile) => void;
};

function ResponseProfileCard({ profile, onEdit }: ResponseProfileCardProps): ReactElement {
  return (
    <Card
      title={<span data-testid="response-profile-name">{profile.name}</span>}
      type={cardType.default}
      className="curtailment-response-profile-card bg-surface-elevated-base shadow-100"
      headerTone="neutral"
      headerClassName="items-start bg-surface-elevated-base px-6 pt-6 pb-0"
      titleClassName="truncate text-emphasis-300 leading-5 font-semibold text-text-primary"
      bodyClassName="px-6 pt-0 pb-1"
      testId="response-profile-card"
      headerAction={
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Edit"
          className="!h-8 !px-3 !py-0"
          onClick={() => onEdit(profile)}
          testId={`response-profile-edit-${profile.id}`}
        />
      }
    >
      <div className="space-y-0 text-[14px] leading-[18px] text-text-primary-50">
        <p className="truncate">{profile.targetSummary}</p>
        <p className="truncate">{profile.scope}</p>
      </div>
    </Card>
  );
}

type ResponseProfileCardsProps = {
  profiles: ResponseProfile[];
  isLoading?: boolean;
  loadError?: string | null;
  onEdit: (profile: ResponseProfile) => void;
};

function ResponseProfileCards({
  profiles,
  isLoading = false,
  loadError = null,
  onEdit,
}: ResponseProfileCardsProps): ReactElement {
  if (loadError) {
    return <ResponseProfilesErrorState message={loadError} />;
  }

  if (isLoading) {
    return <ResponseProfilesLoadingState />;
  }

  if (profiles.length === 0) {
    return <ResponseProfilesEmptyState />;
  }

  return (
    <div className="curtailment-response-profile-grid" data-testid="response-profile-card-grid">
      {profiles.map((profile) => (
        <ResponseProfileCard key={profile.id} profile={profile} onEdit={onEdit} />
      ))}
    </div>
  );
}

type SourceModalProps = {
  open: boolean;
  mode?: SourceModalMode;
  initialValues?: CurtailmentSourceFormValues;
  hasSavedPassword?: boolean;
  onDismiss: () => void;
  onSave?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onTestConnection?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onDelete?: () => Promise<void>;
  saving?: boolean;
  testingConnection?: boolean;
  deleting?: boolean;
};

type SourceFormErrors = Partial<Record<keyof CurtailmentSourceFormValues, string>>;
type SourceValidationIntent = "save" | "testConnection";

function SourceModal({
  open,
  mode = "create",
  initialValues = emptySourceFormValues,
  hasSavedPassword = false,
  onDismiss,
  onSave,
  onTestConnection,
  onDelete,
  saving = false,
  testingConnection = false,
  deleting = false,
}: SourceModalProps): ReactElement {
  const [values, setValues] = useState<CurtailmentSourceFormValues>(() => initialValues);
  const [passwordPlaceholderActive, setPasswordPlaceholderActive] = useState(() => mode === "edit" && hasSavedPassword);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [validationIntent, setValidationIntent] = useState<SourceValidationIntent | null>(null);
  const [showConnectionCallout, setShowConnectionCallout] = useState(false);
  const [connectionError, setConnectionError] = useState(false);
  const isEditMode = mode === "edit";
  const isBusy = saving || deleting || testingConnection;
  const passwordRequired = !isEditMode || sourceCredentialFieldsChanged(values, initialValues);
  const saveValidationErrors = useMemo(
    () => validateSourceFormValues(values, passwordRequired),
    [passwordRequired, values],
  );
  const testConnectionValidationErrors = useMemo(() => validateSourceFormValues(values, true), [values]);
  const visibleValidationErrors =
    validationIntent === "testConnection"
      ? testConnectionValidationErrors
      : validationIntent === "save"
        ? saveValidationErrors
        : {};
  const canSave = Object.keys(saveValidationErrors).length === 0;
  const canTestConnection = Object.keys(testConnectionValidationErrors).length === 0;
  const showSavedPasswordPlaceholder = isEditMode && hasSavedPassword && passwordPlaceholderActive;
  const passwordInputValue = showSavedPasswordPlaceholder ? savedPasswordPlaceholder : values.password;
  const showConnectionSuccessCallout = showConnectionCallout && !testingConnection && !connectionError;
  const showConnectionFailureCallout = showConnectionCallout && !testingConnection && connectionError;

  const updateSourceValue = useCallback((value: string, id: string) => {
    const formKey = sourceInputIdToFormKey[id];
    if (!formKey) {
      return;
    }

    setValues((currentValues) => ({
      ...currentValues,
      [formKey]: value,
    }));
    setShowConnectionCallout(false);
  }, []);

  const handlePasswordFocus = useCallback(() => {
    if (!showSavedPasswordPlaceholder) {
      return;
    }

    setPasswordPlaceholderActive(false);
    setValues((currentValues) => ({
      ...currentValues,
      password: "",
    }));
  }, [showSavedPasswordPlaceholder]);

  const handleSave = useCallback(async () => {
    if (isBusy) {
      return;
    }

    if (!canSave) {
      if (showSavedPasswordPlaceholder && saveValidationErrors.password) {
        setPasswordPlaceholderActive(false);
      }
      setValidationIntent("save");
      return;
    }

    try {
      setSaveError(null);
      await onSave?.(values);
      onDismiss();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to save source."));
    }
  }, [canSave, isBusy, onDismiss, onSave, saveValidationErrors, showSavedPasswordPlaceholder, values]);

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

  const handleTestConnection = useCallback(async () => {
    if (isBusy || !onTestConnection) {
      return;
    }

    if (!canTestConnection) {
      if (showSavedPasswordPlaceholder && testConnectionValidationErrors.password) {
        setPasswordPlaceholderActive(false);
      }
      setValidationIntent("testConnection");
      return;
    }

    try {
      setSaveError(null);
      setConnectionError(false);
      await onTestConnection(values);
      setConnectionError(false);
    } catch {
      setConnectionError(true);
    } finally {
      setShowConnectionCallout(true);
    }
  }, [
    canTestConnection,
    isBusy,
    onTestConnection,
    showSavedPasswordPlaceholder,
    testConnectionValidationErrors,
    values,
  ]);

  const handleDelete = useCallback(async () => {
    if (!onDelete || isBusy) {
      return;
    }

    try {
      setSaveError(null);
      await onDelete();
      onDismiss();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to delete source."));
    }
  }, [isBusy, onDelete, onDismiss]);

  return (
    <Modal
      open={open}
      title={isEditMode ? "Edit source" : "Add source"}
      description={SOURCES_DESCRIPTION}
      onDismiss={onDismiss}
      size={modalSizes.standard}
      divider={false}
      testId="curtailment-source-modal"
      buttons={[
        ...(isEditMode && onDelete
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
          text: "Test connection",
          variant: variants.secondary,
          className: "whitespace-nowrap overflow-clip",
          testId: "curtailment-source-test-connection-button",
          disabled: isBusy || !onTestConnection,
          loading: testingConnection,
          dismissModalOnClick: false,
          onClick: () => void handleTestConnection(),
        },
        {
          text: "Save",
          variant: variants.primary,
          disabled: isBusy,
          loading: saving,
          dismissModalOnClick: false,
          onClick: () => void handleSave(),
        },
      ]}
      bodyClassName="text-text-primary"
    >
      <div className="grid gap-3 pb-2" onKeyDown={handleFormKeyDown}>
        <DismissibleCalloutWrapper
          icon={<Success />}
          intent={intents.success}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showConnectionSuccessCallout}
          title="Source connection successful"
          testId="curtailment-source-connected-callout"
        />
        <DismissibleCalloutWrapper
          icon={<Alert width={iconSizes.medium} />}
          intent={intents.danger}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showConnectionFailureCallout}
          title={SOURCE_CONNECTION_FAILURE_MESSAGE}
          testId="curtailment-source-not-connected-callout"
        />
        {saveError ? (
          <div className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical">{saveError}</div>
        ) : null}
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.name}
            label="Configuration name"
            initValue={values.name}
            error={visibleValidationErrors.name}
            onChange={updateSourceValue}
          />
          <Input id="source-type" label="Integration" initValue="MaestroOS" disabled />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.brokerPrimaryHost}
            label="Broker host 1"
            initValue={values.brokerPrimaryHost}
            error={visibleValidationErrors.brokerPrimaryHost}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.brokerSecondaryHost}
            label="Broker host 2"
            initValue={values.brokerSecondaryHost}
            error={visibleValidationErrors.brokerSecondaryHost}
            onChange={updateSourceValue}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.brokerPort}
            label="Port"
            type="number"
            inputMode="numeric"
            initValue={values.brokerPort}
            error={visibleValidationErrors.brokerPort}
            onChange={updateSourceValue}
            tooltip={{
              body: "Default MQTT port for MaestroOS is 1883.",
              position: positions["top right"],
              widthClassName: "w-72",
            }}
          />
          <Input
            id={sourceInputIds.topic}
            label="Topic"
            initValue={values.topic}
            error={visibleValidationErrors.topic}
            onChange={updateSourceValue}
            tooltip={{
              body: "The MQTT topic to subscribe to on MaestroOS for curtailment signals.",
              widthClassName: "w-72",
            }}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.username}
            label="Username"
            initValue={values.username}
            error={visibleValidationErrors.username}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.password}
            label="Password"
            type="password"
            initValue={passwordInputValue}
            error={visibleValidationErrors.password}
            onChange={updateSourceValue}
            onFocus={handlePasswordFocus}
            hidePasswordToggle={showSavedPasswordPlaceholder}
          />
        </div>
      </div>
    </Modal>
  );
}

type SectionHeaderProps = {
  title: string;
  buttonText: string;
  onButtonClick: () => void;
  infoToggle?: ReactElement;
};

function SectionHeader({ title, buttonText, onButtonClick, infoToggle }: SectionHeaderProps): ReactElement {
  return (
    <div className="curtailment-section-header">
      <div className="curtailment-section-header__title">
        <h2 className="curtailment-section-header__label">{title}</h2>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {infoToggle}
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text={buttonText}
          onClick={onButtonClick}
          className="curtailment-settings__action-button"
        />
      </div>
    </div>
  );
}

type CurtailmentSourceColConfigOptions = {
  onToggle: (sourceId: string) => void;
  updatingSourceIds: ReadonlySet<string>;
};

function createCurtailmentSourceColConfig({
  onToggle,
  updatingSourceIds,
}: CurtailmentSourceColConfigOptions): ColConfig<CurtailmentSource, string, CurtailmentSourceColumn> {
  return {
    [curtailmentSourceCols.name]: {
      component: (source) => (
        <span className="block max-w-full truncate text-emphasis-300 text-text-primary">{source.name}</span>
      ),
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.lastSignalValue]: {
      component: (source) => <span className="truncate text-text-primary">{source.lastTarget}</span>,
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.lastSignalUpdate]: {
      component: (source) => <span className="truncate text-text-primary">{source.lastSeen}</span>,
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.health]: {
      component: (source) => (
        <div className="inline-flex items-center gap-1.5">
          <span
            className={clsx(
              "curtailment-source-health-dot h-2 w-2 shrink-0 rounded-full",
              sourceHealthDotClassName[source.health],
              source.health === "connected" && "curtailment-source-health-dot--connected",
            )}
          />
          <span className="truncate text-text-primary">{formatSourceHealth(source.health)}</span>
        </div>
      ),
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.enabled]: {
      component: (source) => (
        <div className="flex justify-end" data-interactive>
          <Switch
            checked={source.enabled}
            setChecked={() => onToggle(source.id)}
            disabled={updatingSourceIds.has(source.id)}
          />
        </div>
      ),
      width: "w-[6%] phone:w-9",
    },
  };
}

type CurtailmentSettingsContentProps = {
  initialResponseProfiles?: ResponseProfile[];
  initialResponseProfileModalOpen?: boolean;
  initialSources?: CurtailmentSource[];
  initialSourceModalOpen?: boolean;
  initialAutomationRules?: AutomationRule[];
  responseProfiles?: ResponseProfile[];
  sources?: CurtailmentSource[];
  automationRules?: AutomationRule[];
  isLoadingResponseProfiles?: boolean;
  loadResponseProfilesError?: string | null;
  isLoadingSources?: boolean;
  loadSourcesError?: string | null;
  isLoadingAutomationRules?: boolean;
  loadAutomationRulesError?: string | null;
  isSavingResponseProfile?: boolean;
  isTestingResponseProfileCurtailment?: boolean;
  isDeletingResponseProfile?: boolean;
  isSavingSource?: boolean;
  isTestingSourceConnection?: boolean;
  isSavingAutomationRule?: boolean;
  updatingResponseProfileIds?: ReadonlySet<string>;
  updatingSourceIds?: ReadonlySet<string>;
  updatingAutomationRuleIds?: ReadonlySet<string>;
  onCreateResponseProfile?: (values: ResponseProfileFormValues) => Promise<ResponseProfile | void>;
  onUpdateResponseProfile?: (
    profile: ResponseProfile,
    values: ResponseProfileFormValues,
  ) => Promise<ResponseProfile | void>;
  onTestResponseProfileCurtailment?: (
    values: ResponseProfileFormValues,
    curtailmentValues: CurtailmentSubmitValues,
  ) => Promise<void>;
  onDeleteResponseProfile?: (profile: ResponseProfile) => Promise<void>;
  onCreateSource?: (values: CurtailmentSourceFormValues) => Promise<CurtailmentSource | void>;
  onUpdateSource?: (
    source: CurtailmentSource,
    values: CurtailmentSourceFormValues,
  ) => Promise<CurtailmentSource | void>;
  onTestSourceConnection?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onToggleSource?: (source: CurtailmentSource, enabled: boolean) => Promise<CurtailmentSource | void>;
  onDeleteSource?: (source: CurtailmentSource) => Promise<void>;
  onCreateAutomation?: (values: AutomationRuleFormValues) => Promise<AutomationRule | void>;
  onUpdateAutomation?: (rule: AutomationRule, values: AutomationRuleFormValues) => Promise<AutomationRule | void>;
  onToggleAutomation?: (rule: AutomationRule, enabled: boolean) => Promise<AutomationRule | void>;
  onDeleteAutomation?: (rule: AutomationRule) => Promise<void>;
};

function getSourcesEmptyState(loadSourcesError: string | null, isLoadingSources: boolean): ReactElement {
  if (loadSourcesError) {
    return <SourcesErrorState message={loadSourcesError} />;
  }

  if (isLoadingSources) {
    return <SourcesLoadingState />;
  }

  return <SourcesEmptyState />;
}

export function CurtailmentSettingsContent({
  initialResponseProfiles = emptyResponseProfiles,
  initialResponseProfileModalOpen = false,
  initialSources = emptyCurtailmentSources,
  initialSourceModalOpen = false,
  initialAutomationRules = emptyAutomationRules,
  responseProfiles: controlledResponseProfiles,
  sources: controlledSources,
  automationRules: controlledAutomationRules,
  isLoadingResponseProfiles = false,
  loadResponseProfilesError = null,
  isLoadingSources = false,
  loadSourcesError = null,
  isLoadingAutomationRules = false,
  loadAutomationRulesError = null,
  isSavingResponseProfile = false,
  isTestingResponseProfileCurtailment = false,
  isDeletingResponseProfile = false,
  isSavingSource = false,
  isTestingSourceConnection = false,
  isSavingAutomationRule = false,
  updatingResponseProfileIds = emptyUpdatingResponseProfileIds,
  updatingSourceIds = emptyUpdatingSourceIds,
  updatingAutomationRuleIds = emptyUpdatingAutomationRuleIds,
  onCreateResponseProfile,
  onUpdateResponseProfile,
  onTestResponseProfileCurtailment,
  onDeleteResponseProfile,
  onCreateSource,
  onUpdateSource,
  onTestSourceConnection,
  onToggleSource,
  onDeleteSource,
  onCreateAutomation,
  onUpdateAutomation,
  onToggleAutomation,
  onDeleteAutomation,
}: CurtailmentSettingsContentProps): ReactElement {
  const [localResponseProfiles, setLocalResponseProfiles] = useState<ResponseProfile[]>(() => [
    ...initialResponseProfiles,
  ]);
  const [isResponseProfileModalOpen, setIsResponseProfileModalOpen] = useState(initialResponseProfileModalOpen);
  const [editingResponseProfile, setEditingResponseProfile] = useState<ResponseProfile | null>(null);
  const [responseProfileActionError, setResponseProfileActionError] = useState<string | null>(null);
  const [localSources, setLocalSources] = useState<CurtailmentSource[]>(() => [...initialSources]);
  const [isSourceModalOpen, setIsSourceModalOpen] = useState(initialSourceModalOpen);
  const [editingSource, setEditingSource] = useState<CurtailmentSource | null>(null);
  const responseProfiles = controlledResponseProfiles ?? localResponseProfiles;
  const sources = controlledSources ?? localSources;
  const responseProfileModalMode: ResponseProfileModalMode = editingResponseProfile ? "edit" : "create";
  const responseProfileModalInitialValues = useMemo(
    () =>
      editingResponseProfile
        ? createResponseProfileFormValuesFromProfile(editingResponseProfile)
        : emptyResponseProfileFormValues,
    [editingResponseProfile],
  );
  const responseProfileCurtailmentInitialValues = useMemo(
    () => createCurtailmentFormValuesFromResponseProfile(responseProfileModalInitialValues),
    [responseProfileModalInitialValues],
  );
  const sourceModalMode: SourceModalMode = editingSource ? "edit" : "create";
  const sourceModalInitialValues = useMemo(
    () => (editingSource ? createSourceFormValuesFromSource(editingSource) : emptySourceFormValues),
    [editingSource],
  );
  const isEditingResponseProfile = editingResponseProfile
    ? updatingResponseProfileIds.has(editingResponseProfile.id)
    : false;
  const isEditingSource = editingSource ? updatingSourceIds.has(editingSource.id) : false;

  const openCreateResponseProfileModal = useCallback(() => {
    setResponseProfileActionError(null);
    setEditingResponseProfile(null);
    setIsResponseProfileModalOpen(true);
  }, []);

  const openEditResponseProfileModal = useCallback((profile: ResponseProfile) => {
    setResponseProfileActionError(null);
    setEditingResponseProfile(profile);
    setIsResponseProfileModalOpen(true);
  }, []);

  const closeResponseProfileModal = useCallback(() => {
    setResponseProfileActionError(null);
    setIsResponseProfileModalOpen(false);
    setEditingResponseProfile(null);
  }, []);

  const openCreateSourceModal = useCallback(() => {
    setEditingSource(null);
    setIsSourceModalOpen(true);
  }, []);

  const openEditSourceModal = useCallback((source: CurtailmentSource) => {
    setEditingSource(source);
    setIsSourceModalOpen(true);
  }, []);

  const closeSourceModal = useCallback(() => {
    setIsSourceModalOpen(false);
    setEditingSource(null);
  }, []);

  const handleCreateResponseProfile = useCallback(
    async (values: ResponseProfileFormValues) => {
      const createdProfile = await onCreateResponseProfile?.(values);
      if (!controlledResponseProfiles) {
        setLocalResponseProfiles((currentProfiles) => {
          const profile = createdProfile ?? createResponseProfileFromFormValues(values, currentProfiles);
          return [...currentProfiles.filter((currentProfile) => currentProfile.id !== profile.id), profile];
        });
      }
    },
    [controlledResponseProfiles, onCreateResponseProfile],
  );

  const handleSaveResponseProfile = useCallback(
    async (values: ResponseProfileFormValues) => {
      if (!editingResponseProfile) {
        await handleCreateResponseProfile(values);
        return;
      }

      const updatedProfile =
        (await onUpdateResponseProfile?.(editingResponseProfile, values)) ??
        createResponseProfileFromFormValues(values, responseProfiles, editingResponseProfile);
      if (!controlledResponseProfiles) {
        setLocalResponseProfiles((currentProfiles) =>
          currentProfiles.map((currentProfile) =>
            currentProfile.id === updatedProfile.id ? updatedProfile : currentProfile,
          ),
        );
      }
    },
    [
      controlledResponseProfiles,
      editingResponseProfile,
      handleCreateResponseProfile,
      onUpdateResponseProfile,
      responseProfiles,
    ],
  );

  const handleSaveResponseProfileFromCurtailment = useCallback(
    async (values: CurtailmentSubmitValues) => {
      await handleSaveResponseProfile(createResponseProfileFormValuesFromCurtailmentValues(values));
      closeResponseProfileModal();
    },
    [closeResponseProfileModal, handleSaveResponseProfile],
  );

  const handleTestResponseProfileCurtailmentFromCurtailment = useCallback(
    async (values: CurtailmentSubmitValues) => {
      const responseProfileValues = createResponseProfileFormValuesFromCurtailmentValues(values);

      await handleSaveResponseProfile(responseProfileValues);
      await onTestResponseProfileCurtailment?.(responseProfileValues, values);
      closeResponseProfileModal();
    },
    [closeResponseProfileModal, handleSaveResponseProfile, onTestResponseProfileCurtailment],
  );

  const handleDeleteResponseProfile = useCallback(async () => {
    if (!editingResponseProfile) {
      return;
    }

    await onDeleteResponseProfile?.(editingResponseProfile);
    if (!controlledResponseProfiles) {
      setLocalResponseProfiles((currentProfiles) =>
        currentProfiles.filter((currentProfile) => currentProfile.id !== editingResponseProfile.id),
      );
    }
  }, [controlledResponseProfiles, editingResponseProfile, onDeleteResponseProfile]);

  const handleDeleteResponseProfileFromCurtailment = useCallback(async () => {
    await handleDeleteResponseProfile();
    closeResponseProfileModal();
  }, [closeResponseProfileModal, handleDeleteResponseProfile]);

  const handleResponseProfileModalSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      setResponseProfileActionError(null);
      void handleSaveResponseProfileFromCurtailment(values).catch((error) => {
        setResponseProfileActionError(getErrorMessage(error, "Failed to save response profile."));
      });
    },
    [handleSaveResponseProfileFromCurtailment],
  );

  const handleResponseProfileModalTestCurtailment = useCallback(
    (values: CurtailmentSubmitValues) => {
      setResponseProfileActionError(null);
      void handleTestResponseProfileCurtailmentFromCurtailment(values).catch((error) => {
        setResponseProfileActionError(getErrorMessage(error, "Failed to run curtailment."));
      });
    },
    [handleTestResponseProfileCurtailmentFromCurtailment],
  );

  const handleResponseProfileModalDelete = useCallback(() => {
    setResponseProfileActionError(null);
    void handleDeleteResponseProfileFromCurtailment().catch((error) => {
      setResponseProfileActionError(getErrorMessage(error, "Failed to delete response profile."));
    });
  }, [handleDeleteResponseProfileFromCurtailment]);

  const toggleSource = useCallback(
    (sourceId: string) => {
      const source = sources.find((currentSource) => currentSource.id === sourceId);
      if (!source) {
        return;
      }

      const nextEnabled = !source.enabled;
      if (onToggleSource) {
        void onToggleSource(source, nextEnabled).catch(() => {});
        return;
      }

      setLocalSources((currentSources) =>
        currentSources.map((currentSource) =>
          currentSource.id === sourceId ? { ...currentSource, enabled: nextEnabled } : currentSource,
        ),
      );
    },
    [onToggleSource, sources],
  );

  const handleCreateSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      const createdSource = await onCreateSource?.(values);
      if (!controlledSources && createdSource) {
        setLocalSources((currentSources) => [
          ...currentSources.filter((currentSource) => currentSource.id !== createdSource.id),
          createdSource,
        ]);
      }
    },
    [controlledSources, onCreateSource],
  );

  const handleSaveSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      if (!editingSource) {
        await handleCreateSource(values);
        return;
      }

      const updatedSource =
        (await onUpdateSource?.(editingSource, values)) ?? applySourceFormValues(editingSource, values);
      if (!controlledSources) {
        setLocalSources((currentSources) =>
          currentSources.map((currentSource) =>
            currentSource.id === updatedSource.id ? updatedSource : currentSource,
          ),
        );
      }
    },
    [controlledSources, editingSource, handleCreateSource, onUpdateSource],
  );

  const handleDeleteSource = useCallback(async () => {
    if (!editingSource) {
      return;
    }

    await onDeleteSource?.(editingSource);
    if (!controlledSources) {
      setLocalSources((currentSources) =>
        currentSources.filter((currentSource) => currentSource.id !== editingSource.id),
      );
    }
  }, [controlledSources, editingSource, onDeleteSource]);

  const sourceColConfig = useMemo(
    () =>
      createCurtailmentSourceColConfig({
        onToggle: toggleSource,
        updatingSourceIds,
      }),
    [toggleSource, updatingSourceIds],
  );

  const emptyStateRow = getSourcesEmptyState(loadSourcesError, isLoadingSources);

  return (
    <div className="flex flex-col gap-14" data-testid="settings-curtailment-page">
      <Header title="Curtailment" titleSize="text-heading-300" description={CURTAILMENT_PAGE_DESCRIPTION} />

      <section className="curtailment-settings__section">
        <SectionHeader
          title="Response profiles"
          buttonText="Create profile"
          onButtonClick={openCreateResponseProfileModal}
          infoToggle={<ResponseProfilesInfoToggle />}
        />
        <ResponseProfileCards
          profiles={responseProfiles}
          isLoading={isLoadingResponseProfiles}
          loadError={loadResponseProfilesError}
          onEdit={openEditResponseProfileModal}
        />
      </section>

      <section className="curtailment-settings__section">
        <SectionHeader
          title="Sources"
          buttonText="Add source"
          onButtonClick={openCreateSourceModal}
          infoToggle={<SourcesInfoToggle />}
        />
        <List<CurtailmentSource, string, CurtailmentSourceColumn>
          activeCols={activeCurtailmentSourceCols}
          colTitles={curtailmentSourceColTitles}
          columnHeaderAriaLabels={curtailmentSourceColumnAriaLabels}
          colConfig={sourceColConfig}
          items={sources}
          itemKey="id"
          total={sources.length}
          hideTotal
          itemName={{ singular: "source", plural: "sources" }}
          stickyFirstColumn={false}
          isRowDisabled={(source) => !source.enabled}
          columnsExemptFromDisabledStyling={curtailmentSourceColumnsExemptFromDisabledStyling}
          tableClassName={curtailmentSourcesTableClassName}
          emptyStateRow={emptyStateRow}
          applyColumnWidthsToCells
          onRowClick={openEditSourceModal}
        />
      </section>

      <CurtailmentAutomationsContent
        initialAutomationRules={initialAutomationRules}
        automationRules={controlledAutomationRules}
        sources={sources}
        responseProfiles={responseProfiles}
        isLoading={isLoadingAutomationRules}
        loadError={loadAutomationRulesError}
        isCreating={isSavingAutomationRule}
        updatingRuleIds={updatingAutomationRuleIds}
        isLoadingSources={isLoadingSources}
        loadSourcesError={loadSourcesError}
        isLoadingResponseProfiles={isLoadingResponseProfiles}
        loadResponseProfilesError={loadResponseProfilesError}
        onCreateAutomation={onCreateAutomation}
        onUpdateAutomation={onUpdateAutomation}
        onToggleAutomation={onToggleAutomation}
        onDeleteAutomation={onDeleteAutomation}
      />

      <CurtailmentStartModal
        key={
          isResponseProfileModalOpen
            ? `response-profile-${responseProfileModalMode}-${editingResponseProfile?.id ?? "new"}`
            : "response-profile-modal-closed"
        }
        open={isResponseProfileModalOpen}
        variant="responseProfile"
        responseProfileMode={responseProfileModalMode}
        initialValues={responseProfileCurtailmentInitialValues}
        actionError={responseProfileActionError}
        onDismiss={closeResponseProfileModal}
        onSubmit={handleResponseProfileModalSubmit}
        onTestCurtailment={onTestResponseProfileCurtailment ? handleResponseProfileModalTestCurtailment : undefined}
        onDeleteResponseProfile={editingResponseProfile ? handleResponseProfileModalDelete : undefined}
        isSubmitting={editingResponseProfile ? isEditingResponseProfile : isSavingResponseProfile}
        isTestingCurtailment={isTestingResponseProfileCurtailment}
        isDeleting={editingResponseProfile ? isDeletingResponseProfile || isEditingResponseProfile : false}
      />

      <SourceModal
        key={isSourceModalOpen ? `source-modal-${editingSource?.id ?? "new"}` : "source-modal-closed"}
        open={isSourceModalOpen}
        mode={sourceModalMode}
        initialValues={sourceModalInitialValues}
        hasSavedPassword={editingSource?.hasPassword ?? false}
        onDismiss={closeSourceModal}
        onSave={handleSaveSource}
        onTestConnection={onTestSourceConnection}
        onDelete={editingSource ? handleDeleteSource : undefined}
        saving={editingSource ? isEditingSource : isSavingSource}
        testingConnection={isTestingSourceConnection}
        deleting={isEditingSource}
      />
    </div>
  );
}

function CurtailmentSettingsPage(): ReactElement {
  const canManageCurtailment = useHasPermission("curtailment:manage");
  const navigate = useNavigate();
  const { startCurtailment } = useCurtailmentApi();
  const [isTestingResponseProfileCurtailment, setIsTestingResponseProfileCurtailment] = useState(false);
  const {
    responseProfiles,
    isLoading: isLoadingResponseProfiles,
    isCreating: isCreatingResponseProfile,
    updatingProfileIds,
    loadError: responseProfilesLoadError,
    createResponseProfile,
    updateResponseProfile,
    deleteResponseProfile,
  } = useCurtailmentResponseProfiles(canManageCurtailment);
  const {
    sources,
    isLoading,
    isCreating,
    updatingSourceIds,
    loadError,
    createSource,
    updateSource,
    testConnection,
    isTestingConnection,
    setSourceEnabled,
    deleteSource,
  } = useMqttCurtailmentSources(canManageCurtailment);
  const {
    automationRules,
    isLoading: isLoadingAutomationRules,
    isCreating: isCreatingAutomationRule,
    updatingRuleIds: updatingAutomationRuleIds,
    loadError: automationRulesLoadError,
    createAutomationRule,
    updateAutomationRule,
    setAutomationRuleEnabled,
    deleteAutomationRule,
  } = useCurtailmentAutomationRules(canManageCurtailment);

  useEffect(() => {
    if (!loadError) {
      return;
    }

    pushToast({
      message: loadError,
      status: STATUSES.error,
    });
  }, [loadError]);

  useEffect(() => {
    if (!responseProfilesLoadError) {
      return;
    }

    pushToast({
      message: responseProfilesLoadError,
      status: STATUSES.error,
    });
  }, [responseProfilesLoadError]);

  useEffect(() => {
    if (!automationRulesLoadError) {
      return;
    }

    pushToast({
      message: automationRulesLoadError,
      status: STATUSES.error,
    });
  }, [automationRulesLoadError]);

  const handleCreateResponseProfile = useCallback(
    async (values: ResponseProfileFormValues) => {
      const profile = await createResponseProfile(values);
      pushToast({
        message: "Response profile added",
        status: STATUSES.success,
      });
      return profile;
    },
    [createResponseProfile],
  );

  const handleUpdateResponseProfile = useCallback(
    async (profile: ResponseProfile, values: ResponseProfileFormValues) => {
      const updatedProfile = await updateResponseProfile(profile.id, values);
      pushToast({
        message: "Response profile saved",
        status: STATUSES.success,
      });
      return updatedProfile;
    },
    [updateResponseProfile],
  );

  const handleTestResponseProfileCurtailment = useCallback(
    async (_values: ResponseProfileFormValues, curtailmentValues: CurtailmentSubmitValues) => {
      setIsTestingResponseProfileCurtailment(true);

      try {
        await startCurtailment(curtailmentValues);
        setIsTestingResponseProfileCurtailment(false);
        navigate("/energy");
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to run curtailment."),
          status: STATUSES.error,
        });
        setIsTestingResponseProfileCurtailment(false);
        throw error;
      }
    },
    [navigate, startCurtailment],
  );

  const handleDeleteResponseProfile = useCallback(
    async (profile: ResponseProfile) => {
      await deleteResponseProfile(profile.id);
      pushToast({
        message: "Response profile deleted",
        status: STATUSES.success,
      });
    },
    [deleteResponseProfile],
  );

  const handleCreateSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      const source = await createSource(values);
      pushToast({
        message: "Source added",
        status: STATUSES.success,
      });
      return source;
    },
    [createSource],
  );

  const handleToggleSource = useCallback(
    async (source: CurtailmentSource, enabled: boolean) => {
      try {
        return await setSourceEnabled(source.id, enabled);
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update source."),
          status: STATUSES.error,
        });
        throw error;
      }
    },
    [setSourceEnabled],
  );

  const handleUpdateSource = useCallback(
    async (source: CurtailmentSource, values: CurtailmentSourceFormValues) => {
      const updatedSource = await updateSource(source.id, values);
      pushToast({
        message: "Source saved",
        status: STATUSES.success,
      });
      return updatedSource;
    },
    [updateSource],
  );

  const handleTestSourceConnection = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      await testConnection(values);
    },
    [testConnection],
  );

  const handleDeleteSource = useCallback(
    async (source: CurtailmentSource) => {
      await deleteSource(source.id);
      pushToast({
        message: "Source deleted",
        status: STATUSES.success,
      });
    },
    [deleteSource],
  );

  const handleCreateAutomation = useCallback(
    async (values: AutomationRuleFormValues) => {
      const rule = await createAutomationRule(values);
      pushToast({
        message: "Automation added",
        status: STATUSES.success,
      });
      return rule;
    },
    [createAutomationRule],
  );

  const handleUpdateAutomation = useCallback(
    async (rule: AutomationRule, values: AutomationRuleFormValues) => {
      const updatedRule = await updateAutomationRule(rule.id, values);
      pushToast({
        message: "Automation saved",
        status: STATUSES.success,
      });
      return updatedRule;
    },
    [updateAutomationRule],
  );

  const handleToggleAutomation = useCallback(
    async (rule: AutomationRule, enabled: boolean) => {
      try {
        return await setAutomationRuleEnabled(rule.id, enabled);
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update automation."),
          status: STATUSES.error,
        });
        throw error;
      }
    },
    [setAutomationRuleEnabled],
  );

  const handleDeleteAutomation = useCallback(
    async (rule: AutomationRule) => {
      await deleteAutomationRule(rule.id);
      pushToast({
        message: "Automation deleted",
        status: STATUSES.success,
      });
    },
    [deleteAutomationRule],
  );

  if (!canManageCurtailment) {
    return <Navigate to="/settings/general" replace />;
  }

  return (
    <CurtailmentSettingsContent
      responseProfiles={responseProfiles}
      sources={sources}
      automationRules={automationRules}
      isLoadingResponseProfiles={isLoadingResponseProfiles}
      loadResponseProfilesError={responseProfilesLoadError}
      isLoadingSources={isLoading}
      loadSourcesError={loadError}
      isLoadingAutomationRules={isLoadingAutomationRules}
      loadAutomationRulesError={automationRulesLoadError}
      isSavingResponseProfile={isCreatingResponseProfile}
      isTestingResponseProfileCurtailment={isTestingResponseProfileCurtailment}
      isSavingSource={isCreating}
      isTestingSourceConnection={isTestingConnection}
      isSavingAutomationRule={isCreatingAutomationRule}
      updatingResponseProfileIds={updatingProfileIds}
      updatingSourceIds={updatingSourceIds}
      updatingAutomationRuleIds={updatingAutomationRuleIds}
      onCreateResponseProfile={handleCreateResponseProfile}
      onUpdateResponseProfile={handleUpdateResponseProfile}
      onTestResponseProfileCurtailment={handleTestResponseProfileCurtailment}
      onDeleteResponseProfile={handleDeleteResponseProfile}
      onCreateSource={handleCreateSource}
      onUpdateSource={handleUpdateSource}
      onTestSourceConnection={handleTestSourceConnection}
      onToggleSource={handleToggleSource}
      onDeleteSource={handleDeleteSource}
      onCreateAutomation={handleCreateAutomation}
      onUpdateAutomation={handleUpdateAutomation}
      onToggleAutomation={handleToggleAutomation}
      onDeleteAutomation={handleDeleteAutomation}
    />
  );
}

export default CurtailmentSettingsPage;
