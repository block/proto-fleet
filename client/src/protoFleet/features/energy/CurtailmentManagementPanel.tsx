import { type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";

import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildSiteNameById } from "@/protoFleet/api/siteNames";
import { useSites } from "@/protoFleet/api/sites";
import {
  adminTerminateReasonRequiredMessage,
  type ForceReleaseCurtailmentOptions,
  type AdminTerminateCurtailmentOptions as TerminateRecoveryOptions,
  type AdminTerminateCurtailmentState as TerminateRecoveryState,
  useCurtailmentApi,
} from "@/protoFleet/api/useCurtailmentApi";
import useCurtailmentResponseProfiles from "@/protoFleet/api/useCurtailmentResponseProfiles";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import ActiveCurtailmentStatus, {
  type ActiveCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentEventState } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CurtailmentHistory, { type CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import { getDefaultCurtailmentSiteScope } from "@/protoFleet/features/energy/curtailmentSiteScopeDefaults";
import CurtailmentStartModal, {
  type CurtailmentPlanPreview,
  type CurtailmentResponseProfileOption,
  type CurtailmentSiteOption,
  type CurtailmentStartModalMode,
  type CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { createCurtailmentPlanPreview } from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import type {
  ResponseProfile,
  ResponseProfileFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Radio from "@/shared/components/Radio";
import Textarea from "@/shared/components/Textarea";

interface CurtailmentManagementPanelProps {
  enableManage?: boolean;
  enableRecover?: boolean;
  className?: string;
}

interface PendingStopConfirmation {
  action: CurtailmentStopConfirmationAction;
  eventId: string;
}

interface EditCurtailmentSession {
  eventId: string;
  initialValues: CurtailmentSubmitValues;
  preview: CurtailmentPlanPreview;
}

interface CurtailmentMessageProps {
  message: string;
}

interface TerminateRecoveryDialogProps {
  error?: string | null;
  isSubmitting?: boolean;
  onCancel: () => void;
  onConfirm: (options: TerminateRecoveryOptions) => void;
  open: boolean;
}

interface ForceReleaseDialogProps {
  error?: string | null;
  isSubmitting?: boolean;
  mode: "curtailment" | "restore";
  onCancel: () => void;
  onConfirm: (options: ForceReleaseCurtailmentOptions) => void;
  open: boolean;
}

const activeCurtailmentRefreshIntervalMs = 3_000;
const nonTerminalActiveEventStates = new Set<CurtailmentEventState>(["pending", "active", "restoring"]);
const updateableCurtailmentEventStates = new Set<CurtailmentEventState>(["pending", "active"]);
const forceRestorableCurtailmentEventStates = new Set<CurtailmentEventState>(["pending", "active"]);
const defaultResponseDeadlineMinutes = "15";
const defaultMaxDurationSec = "900";
const immediateRestoreBatchSize = "10000";

const terminateRecoveryStateOptions: { label: string; value: TerminateRecoveryState }[] = [
  { label: "Cancelled", value: "cancelled" },
  { label: "Failed", value: "failed" },
];
const automationRestoreBlockedErrorPrefix = "cannot restore automation-owned curtailment event";

function getRecoveryStopErrorMessage(
  stopError: string | null,
  isAutomationOwned: boolean | undefined,
  canUseRecovery: boolean,
): string | null {
  if (!stopError) {
    return null;
  }
  if (!isAutomationOwned) {
    return stopError;
  }
  if (stopError.startsWith(automationRestoreBlockedErrorPrefix)) {
    return canUseRecovery
      ? "Automation is still requesting curtailment. Use Abort to cancel this event and disable the automation before restoring miners."
      : "Automation is still requesting curtailment. Ask an admin to Abort this event and disable the automation before restoring miners.";
  }
  return stopError;
}

function minutesToSeconds(value: string): string {
  const minutes = Number(value);

  if (!Number.isFinite(minutes) || minutes <= 0) {
    return defaultMaxDurationSec;
  }

  return String(minutes * 60);
}

function createSiteOptions(sites: SiteWithCounts[]): CurtailmentSiteOption[] {
  return sites
    .flatMap((siteWithCounts) => {
      const site = siteWithCounts.site;
      const id = site?.id ? site.id.toString() : "";

      return id ? [{ id, name: site?.name || `Site ${id}` }] : [];
    })
    .sort((left, right) => left.name.localeCompare(right.name, undefined, { sensitivity: "base" }));
}

function uniqueNonEmptyStrings(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function getSelectedResponseProfileSiteIds(
  values: Pick<ResponseProfileFormValues, "siteSelection" | "siteId" | "siteIds">,
): string[] {
  const siteIds = uniqueNonEmptyStrings(
    values.siteIds !== undefined && values.siteIds.length > 0 ? values.siteIds : values.siteId ? [values.siteId] : [],
  );

  return values.siteSelection === "site" ||
    values.siteSelection === "allSites" ||
    (values.siteSelection === undefined && siteIds.length > 0)
    ? siteIds
    : [];
}

function getResponseProfileSiteNameForId(values: Partial<ResponseProfileFormValues>, siteId: string): string {
  return values.siteNamesById?.[siteId]?.trim() || (values.siteId === siteId ? values.siteName?.trim() : "") || "";
}

function getResponseProfileSiteNamesById(
  values: ResponseProfileFormValues,
  siteIds: readonly string[],
): Record<string, string> {
  return Object.fromEntries(
    siteIds.map((siteId) => [siteId, getResponseProfileSiteNameForId(values, siteId) || `Site ${siteId}`]),
  );
}

function createResponseProfileFormValuesFromProfile(profile: ResponseProfile): ResponseProfileFormValues {
  if (profile.formValues) {
    const hasAllMinersSelected = profile.formValues.minerSelectionMode === "all";
    const siteIds = hasAllMinersSelected ? [] : getSelectedResponseProfileSiteIds(profile.formValues);
    const siteId = siteIds[0] ?? "";

    return {
      ...profile.formValues,
      deviceIdentifiers: hasAllMinersSelected ? [] : [...profile.formValues.deviceIdentifiers],
      minerSelectionMode: hasAllMinersSelected ? "all" : "subset",
      siteSelection: hasAllMinersSelected
        ? "allSites"
        : profile.formValues.siteSelection === "allSites"
          ? "allSites"
          : siteIds.length > 0
            ? "site"
            : "none",
      siteId,
      siteName: siteId ? getResponseProfileSiteNameForId(profile.formValues, siteId) : "",
      siteIds,
      siteNamesById: getResponseProfileSiteNamesById(profile.formValues, siteIds),
    };
  }

  const targetKwMatch = profile.targetSummary.match(/(\d+(?:\.\d+)?)/);
  const actionType: ResponseProfileFormValues["actionType"] = targetKwMatch ? "fixedKwReduction" : "fullFleet";
  const responseDeadlineMinutes = profile.deadlineSummary.match(/(\d+)/)?.[1] ?? defaultResponseDeadlineMinutes;

  return {
    name: profile.name,
    actionType,
    targetKw: targetKwMatch?.[1] ?? "",
    deviceIdentifiers: [],
    minerSelectionMode: "subset",
    siteSelection: "none",
    siteId: "",
    siteName: "",
    siteIds: [],
    siteNamesById: {},
    selectionStrategy: "leastEfficientFirst",
    restoreBehavior: profile.restoreBehavior.toLowerCase().includes("immediate")
      ? "automaticImmediateRestore"
      : "automaticBatchRestore",
    minDurationSec: "",
    maxDurationSec: minutesToSeconds(responseDeadlineMinutes),
    curtailBatchSize: "",
    curtailBatchIntervalSec: "",
    restoreBatchSize: profile.restoreBehavior.toLowerCase().includes("immediate") ? immediateRestoreBatchSize : "",
    restoreIntervalSec: "",
    responseDeadlineMinutes,
    includeMaintenance: true,
  };
}

function createCurtailmentResponseProfileOption(profile: ResponseProfile): CurtailmentResponseProfileOption {
  const values = createResponseProfileFormValuesFromProfile(profile);
  const restoreBatchSize =
    values.restoreBatchSize ||
    (values.restoreBehavior === "automaticImmediateRestore" ? immediateRestoreBatchSize : "");
  const hasAllMinersSelected = values.minerSelectionMode === "all";
  const siteIds = hasAllMinersSelected ? [] : getSelectedResponseProfileSiteIds(values);
  const siteId = siteIds[0] ?? "";
  const siteNamesById = getResponseProfileSiteNamesById(values, siteIds);
  const siteName = siteId ? siteNamesById[siteId] || `Site ${siteId}` : "";
  const deviceIdentifiers = hasAllMinersSelected ? [] : [...values.deviceIdentifiers];
  const siteSelection = hasAllMinersSelected
    ? "allSites"
    : values.siteSelection === "allSites"
      ? "allSites"
      : siteIds.length > 0
        ? "site"
        : "none";

  return {
    id: profile.id,
    label: profile.name,
    values: {
      scopeType: hasAllMinersSelected
        ? "wholeOrg"
        : deviceIdentifiers.length > 0
          ? "explicitMiners"
          : siteIds.length > 0
            ? "site"
            : "wholeOrg",
      scopeId: hasAllMinersSelected
        ? "whole-org"
        : siteIds.length > 0
          ? siteIds.length === 1
            ? siteName
            : `${siteIds.length} sites`
          : deviceIdentifiers.length > 0
            ? undefined
            : "whole-org",
      siteSelection,
      siteId,
      siteIds,
      siteNamesById,
      deviceSetIds: [],
      deviceIdentifiers,
      minerSelectionMode: hasAllMinersSelected ? "all" : "subset",
      curtailmentMode: values.actionType,
      minerSelectionStrategy: values.selectionStrategy,
      targetKw: values.targetKw,
      curtailBatchSize: values.curtailBatchSize,
      curtailBatchIntervalSec: values.curtailBatchIntervalSec,
      restoreBatchSize,
      restoreIntervalSec: values.restoreIntervalSec,
      includeMaintenance: values.includeMaintenance,
    },
  };
}

function CurtailmentMessage({ message }: CurtailmentMessageProps): ReactElement {
  return (
    <div className="flex items-center gap-3 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
      <Alert className="shrink-0 text-intent-warning-fill" />
      <span className="text-emphasis-300">{message}</span>
    </div>
  );
}

function CurtailmentRecoveryTerminateDialog({
  error,
  isSubmitting = false,
  onCancel,
  onConfirm,
  open,
}: TerminateRecoveryDialogProps): ReactElement {
  const [targetState, setTargetState] = useState<TerminateRecoveryState>("cancelled");
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string | null>(null);
  const validationError = reasonError ?? error ?? null;

  const confirmTerminate = useCallback(() => {
    const trimmedReason = reason.trim();
    if (!trimmedReason) {
      setReasonError(adminTerminateReasonRequiredMessage);
      return;
    }

    setReasonError(null);
    onConfirm({ reason: trimmedReason, targetState });
  }, [onConfirm, reason, targetState]);
  const dismissDialog = isSubmitting ? undefined : onCancel;

  return (
    <Dialog
      open={open}
      title="Terminate recovery event?"
      onDismiss={dismissDialog}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onCancel,
          disabled: isSubmitting,
        },
        {
          text: "Terminate event",
          variant: variants.danger,
          onClick: confirmTerminate,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="grid gap-4 text-300 text-text-primary">
        <p className="text-text-primary-70">
          Only terminate after restore has started. This closes the event audit trail as cancelled or failed.
        </p>
        <fieldset className="grid gap-2">
          <legend className="text-emphasis-300">Target state</legend>
          <div className="flex flex-wrap gap-4">
            {terminateRecoveryStateOptions.map((option) => (
              <label key={option.value} className="flex items-center gap-2">
                <Radio
                  name="terminate-recovery-target-state"
                  value={option.value}
                  selected={targetState === option.value}
                  onChange={() => setTargetState(option.value)}
                  disabled={isSubmitting}
                />
                <span>{option.label}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <Textarea
          id="terminate-recovery-reason"
          label="Reason"
          initValue={reason}
          rows={3}
          maxLength={256}
          required
          error={validationError ?? false}
          onChange={(value) => {
            setReason(value);
            if (value.trim()) {
              setReasonError(null);
            }
          }}
        />
      </div>
    </Dialog>
  );
}

function CurtailmentForceReleaseDialog({
  error,
  isSubmitting = false,
  mode,
  onCancel,
  onConfirm,
  open,
}: ForceReleaseDialogProps): ReactElement {
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string | null>(null);
  const validationError = reasonError ?? error ?? null;
  const title = mode === "restore" ? "Abort restore?" : "Abort curtailment?";
  const confirmText = mode === "restore" ? "Abort restore" : "Abort curtailment";
  const body =
    mode === "restore"
      ? "This aborts the restore workflow by immediately releasing curtailment ownership. If automation owns this event, Abort also disables the automation rule. It does not wake miners or confirm that restore completed."
      : "This cancels the current automation-owned curtailment event and disables the owning automation rule so it cannot immediately curtail miners again. It does not wake miners.";

  const confirmRelease = useCallback(() => {
    const trimmedReason = reason.trim();
    if (!trimmedReason) {
      setReasonError(adminTerminateReasonRequiredMessage);
      return;
    }
    setReasonError(null);
    onConfirm({ reason: trimmedReason });
  }, [onConfirm, reason]);
  const dismissDialog = isSubmitting ? undefined : onCancel;

  return (
    <Dialog
      open={open}
      title={title}
      onDismiss={dismissDialog}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onCancel,
          disabled: isSubmitting,
        },
        {
          text: confirmText,
          variant: variants.danger,
          onClick: confirmRelease,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="grid gap-4 text-300 text-text-primary">
        <p className="text-text-primary-70">{body}</p>
        <Textarea
          id="force-release-reason"
          label="Reason"
          initValue={reason}
          rows={3}
          maxLength={256}
          required
          error={validationError ?? false}
          onChange={(value) => {
            setReason(value);
            if (value.trim()) {
              setReasonError(null);
            }
          }}
        />
      </div>
    </Dialog>
  );
}

function createActiveCurtailmentPreview(
  event: ActiveCurtailmentEvent,
  values: CurtailmentSubmitValues,
): CurtailmentPlanPreview {
  return createCurtailmentPlanPreview(values, {
    selectedMinerCount: event.selectedMiners,
    targetKw: event.targetKw,
    estimatedReductionKw: event.estimatedReductionKw,
  });
}

function canUpdateCurtailmentEvent(event: ActiveCurtailmentEvent): boolean {
  return updateableCurtailmentEventStates.has(event.state);
}

function canTerminateRecoveryCurtailmentEvent(event: ActiveCurtailmentEvent): boolean {
  return Boolean(event.isAutomationOwned && event.state === "restoring");
}

function canAbortCurtailmentOwnership(event: ActiveCurtailmentEvent): boolean {
  return (
    event.state === "restoring" || (event.isAutomationOwned && forceRestorableCurtailmentEventStates.has(event.state))
  );
}

function CurtailmentManagementPanel({
  enableManage = true,
  enableRecover = false,
  className,
}: CurtailmentManagementPanelProps): ReactElement {
  const navigate = useNavigate();
  const canReadSiteCatalog = useHasPermission("site:read");
  const { activeSite } = useActiveSite({});
  const { listSites } = useSites();
  const [loadedSiteNameById, setLoadedSiteNameById] = useState(() => new Map<string, string>());
  const [siteOptions, setSiteOptions] = useState<CurtailmentSiteOption[]>([]);
  const [isLoadingSiteOptions, setIsLoadingSiteOptions] = useState(false);
  const [siteOptionsLoadError, setSiteOptionsLoadError] = useState<string | null>(null);
  /* eslint-disable react-hooks/set-state-in-effect -- site catalog fetch mirrors external permission/load state into modal options */
  useEffect(() => {
    if (!canReadSiteCatalog) {
      setLoadedSiteNameById(new Map());
      setSiteOptions([]);
      setIsLoadingSiteOptions(false);
      setSiteOptionsLoadError(null);
      return undefined;
    }

    const abortController = new AbortController();
    setIsLoadingSiteOptions(true);
    setSiteOptionsLoadError(null);
    void listSites({
      signal: abortController.signal,
      onSuccess: (sites) => {
        if (!abortController.signal.aborted) {
          setLoadedSiteNameById(buildSiteNameById(sites));
          setSiteOptions(createSiteOptions(sites));
        }
      },
      onError: (message) => {
        if (!abortController.signal.aborted) {
          setSiteOptionsLoadError(message || "Couldn't load sites.");
        }
      },
      onFinally: () => {
        if (!abortController.signal.aborted) {
          setIsLoadingSiteOptions(false);
        }
      },
    });

    return () => {
      abortController.abort();
    };
  }, [canReadSiteCatalog, listSites]);
  /* eslint-enable react-hooks/set-state-in-effect */
  const siteNameById = canReadSiteCatalog ? loadedSiteNameById : undefined;
  const {
    activeEvent,
    activeEvents,
    activeEventId,
    activeEventFormValues,
    historyEvents,
    isLoading,
    isStarting,
    isUpdating,
    stoppingEventId,
    adminTerminatingEventId,
    loadError,
    startError,
    updateError,
    stopError,
    adminTerminateError,
    historyCurrentPage,
    historyHasNextPage,
    historyHasPreviousPage,
    historyPageSize,
    historyStatusFilters,
    refreshCurtailment,
    goToHistoryPage,
    setHistoryStatusFilters,
    selectActiveCurtailment,
    startCurtailment,
    dismissTerminalCurtailment,
    updateCurtailment,
    stopCurtailment,
    adminTerminateCurtailment,
    forceReleaseCurtailment,
  } = useCurtailmentApi({ siteNameById });
  const { responseProfiles } = useCurtailmentResponseProfiles(enableManage, { siteNameById });
  const responseProfileOptions = useMemo(
    () => responseProfiles.map(createCurtailmentResponseProfileOption),
    [responseProfiles],
  );
  const defaultSiteScope = useMemo(
    () => (canReadSiteCatalog ? getDefaultCurtailmentSiteScope(activeSite, siteOptions) : undefined),
    [activeSite, canReadSiteCatalog, siteOptions],
  );
  const activeEventIds = useMemo(() => activeEvents.map((event) => event.id), [activeEvents]);
  const [modalMode, setModalMode] = useState<CurtailmentStartModalMode | null>(null);
  const [editSession, setEditSession] = useState<EditCurtailmentSession | null>(null);
  const [pendingStopConfirmation, setPendingStopConfirmation] = useState<PendingStopConfirmation | null>(null);
  const [pendingTerminateRecoveryEventId, setPendingTerminateRecoveryEventId] = useState<string | null>(null);
  const [pendingForceReleaseEventId, setPendingForceReleaseEventId] = useState<string | null>(null);
  const refreshAbortControllerRef = useRef<AbortController | null>(null);
  const activeRefreshAbortControllerRef = useRef<AbortController | null>(null);
  const manageSelectionAbortControllerRef = useRef<AbortController | null>(null);
  const manageSelectionRequestIdRef = useRef(0);
  const foregroundRefreshInFlightRef = useRef(false);
  const canUseRecovery = enableManage && enableRecover;
  const recoveryStopError = getRecoveryStopErrorMessage(stopError, activeEvent?.isAutomationOwned, canUseRecovery);
  const errorMessage = startError ?? updateError ?? recoveryStopError ?? adminTerminateError ?? loadError;
  const isInitialLoading = isLoading && !activeEvent && historyEvents.length === 0;
  const isStopConfirmationSubmitting =
    pendingStopConfirmation !== null && stoppingEventId === pendingStopConfirmation.eventId;
  const isTerminateRecoverySubmitting =
    pendingTerminateRecoveryEventId !== null && adminTerminatingEventId === pendingTerminateRecoveryEventId;
  const isForceReleaseSubmitting =
    pendingForceReleaseEventId !== null && adminTerminatingEventId === pendingForceReleaseEventId;
  const isEditingCurtailment = modalMode === "edit";
  const isModalSubmitting = isEditingCurtailment ? isUpdating : isStarting;
  const hasOngoingCurtailment = activeEvents.some((event) => nonTerminalActiveEventStates.has(event.state));
  const hasOngoingHistoryEvent = historyEvents.some((event) => nonTerminalActiveEventStates.has(event.state));
  const shouldPollCurtailment = hasOngoingCurtailment || hasOngoingHistoryEvent;
  const pendingForceReleaseEvent =
    pendingForceReleaseEventId === null
      ? undefined
      : activeEventId === pendingForceReleaseEventId
        ? activeEvent
        : activeEvents.find((event) => event.id === pendingForceReleaseEventId);
  const pendingForceReleaseMode = pendingForceReleaseEvent?.state === "restoring" ? "restore" : "curtailment";

  const runAbortableRefresh = useCallback(<T,>(operation: (signal: AbortSignal) => Promise<T>) => {
    activeRefreshAbortControllerRef.current?.abort();
    activeRefreshAbortControllerRef.current = null;
    refreshAbortControllerRef.current?.abort();
    const abortController = new AbortController();
    refreshAbortControllerRef.current = abortController;
    foregroundRefreshInFlightRef.current = true;

    return operation(abortController.signal).finally(() => {
      if (refreshAbortControllerRef.current === abortController) {
        refreshAbortControllerRef.current = null;
        foregroundRefreshInFlightRef.current = false;
      }
    });
  }, []);

  useEffect(() => {
    void runAbortableRefresh((signal) => refreshCurtailment({ signal })).catch(() => {});

    return () => refreshAbortControllerRef.current?.abort();
  }, [refreshCurtailment, runAbortableRefresh]);

  useEffect(() => {
    if (!shouldPollCurtailment) {
      return undefined;
    }

    const refreshActiveCurtailment = (): void => {
      if (
        foregroundRefreshInFlightRef.current ||
        refreshAbortControllerRef.current ||
        activeRefreshAbortControllerRef.current
      ) {
        return;
      }

      const abortController = new AbortController();
      activeRefreshAbortControllerRef.current = abortController;

      void refreshCurtailment({ background: true, signal: abortController.signal })
        .catch(() => {})
        .finally(() => {
          if (activeRefreshAbortControllerRef.current === abortController) {
            activeRefreshAbortControllerRef.current = null;
          }
        });
    };

    const intervalId = window.setInterval(() => {
      refreshActiveCurtailment();
    }, activeCurtailmentRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
      activeRefreshAbortControllerRef.current?.abort();
      activeRefreshAbortControllerRef.current = null;
    };
  }, [refreshCurtailment, shouldPollCurtailment]);

  useEffect(
    () => () => {
      manageSelectionAbortControllerRef.current?.abort();
    },
    [],
  );

  const cancelManageSelection = useCallback(() => {
    manageSelectionAbortControllerRef.current?.abort();
    manageSelectionAbortControllerRef.current = null;
    manageSelectionRequestIdRef.current += 1;
  }, []);

  const closeModal = useCallback(() => {
    cancelManageSelection();
    setModalMode(null);
    setEditSession(null);
  }, [cancelManageSelection]);

  const openCreateModal = useCallback(() => {
    cancelManageSelection();
    setEditSession(null);
    setModalMode("create");
  }, [cancelManageSelection]);

  const openEditModal = useCallback(() => {
    if (!enableManage || !activeEvent || !activeEventId || !activeEventFormValues) {
      return;
    }

    cancelManageSelection();
    setEditSession({
      eventId: activeEventId,
      initialValues: activeEventFormValues,
      preview: createActiveCurtailmentPreview(activeEvent, activeEventFormValues),
    });
    setModalMode("edit");
  }, [activeEvent, activeEventFormValues, activeEventId, enableManage, cancelManageSelection]);

  const openHistoryManageModal = useCallback(
    (event: CurtailmentHistoryEvent) => {
      if (!enableManage) {
        return;
      }

      if (
        event.id === activeEventId &&
        activeEvent &&
        activeEventFormValues &&
        canUpdateCurtailmentEvent(activeEvent)
      ) {
        cancelManageSelection();
        setEditSession({
          eventId: activeEventId,
          initialValues: activeEventFormValues,
          preview: createActiveCurtailmentPreview(activeEvent, activeEventFormValues),
        });
        setModalMode("edit");
        return;
      }

      manageSelectionAbortControllerRef.current?.abort();
      const requestId = manageSelectionRequestIdRef.current + 1;
      manageSelectionRequestIdRef.current = requestId;
      const abortController = new AbortController();
      manageSelectionAbortControllerRef.current = abortController;

      void selectActiveCurtailment(event.id, { signal: abortController.signal })
        .then(({ activeEvent: selectedActiveEvent, activeEventId: selectedActiveEventId, activeEventFormValues }) => {
          if (
            abortController.signal.aborted ||
            manageSelectionRequestIdRef.current !== requestId ||
            selectedActiveEventId !== event.id
          ) {
            return;
          }

          if (
            !selectedActiveEvent ||
            !selectedActiveEventId ||
            !activeEventFormValues ||
            !canUpdateCurtailmentEvent(selectedActiveEvent)
          ) {
            return;
          }

          setEditSession({
            eventId: selectedActiveEventId,
            initialValues: activeEventFormValues,
            preview: createActiveCurtailmentPreview(selectedActiveEvent, activeEventFormValues),
          });
          setModalMode("edit");
        })
        .catch(() => {})
        .finally(() => {
          if (manageSelectionAbortControllerRef.current === abortController) {
            manageSelectionAbortControllerRef.current = null;
          }
        });
    },
    [activeEvent, activeEventFormValues, activeEventId, enableManage, cancelManageSelection, selectActiveCurtailment],
  );

  const selectHistoryActiveEvent = useCallback(
    (event: CurtailmentHistoryEvent) => {
      cancelManageSelection();
      const abortController = new AbortController();
      manageSelectionAbortControllerRef.current = abortController;

      void selectActiveCurtailment(event.id, { signal: abortController.signal })
        .catch(() => {})
        .finally(() => {
          if (manageSelectionAbortControllerRef.current === abortController) {
            manageSelectionAbortControllerRef.current = null;
          }
        });
    },
    [cancelManageSelection, selectActiveCurtailment],
  );

  const openStopConfirmation = useCallback(
    (action: CurtailmentStopConfirmationAction, eventId = activeEventId) => {
      if (!enableManage || !eventId) {
        return;
      }

      cancelManageSelection();
      setPendingStopConfirmation({ action, eventId });
    },
    [activeEventId, enableManage, cancelManageSelection],
  );

  const openTerminateRecoveryConfirmation = useCallback(() => {
    if (!canUseRecovery || !activeEvent || !activeEventId || !canTerminateRecoveryCurtailmentEvent(activeEvent)) {
      return;
    }

    cancelManageSelection();
    setPendingTerminateRecoveryEventId(activeEventId);
  }, [activeEvent, activeEventId, canUseRecovery, cancelManageSelection]);

  const handleStartSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      void startCurtailment(values)
        .then(closeModal)
        .catch(() => {});
    },
    [closeModal, startCurtailment],
  );

  const handleUpdateSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      const editEventId = editSession?.eventId ?? activeEventId;
      if (!editEventId) {
        return;
      }

      void updateCurtailment(editEventId, values, editSession?.initialValues ?? activeEventFormValues ?? undefined)
        .then(closeModal)
        .catch(() => {});
    },
    [activeEventFormValues, activeEventId, closeModal, editSession, updateCurtailment],
  );

  const handleModalSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      if (isEditingCurtailment) {
        handleUpdateSubmit(values);
        return;
      }

      handleStartSubmit(values);
    },
    [handleStartSubmit, handleUpdateSubmit, isEditingCurtailment],
  );

  const handleHistoryStop = useCallback(
    (event: CurtailmentHistoryEvent) => {
      cancelManageSelection();
      return stopCurtailment(event.id);
    },
    [cancelManageSelection, stopCurtailment],
  );

  const handleHistoryPageChange = useCallback(
    (historyPage: number) => {
      void runAbortableRefresh((signal) => goToHistoryPage(historyPage, { signal })).catch(() => {});
    },
    [goToHistoryPage, runAbortableRefresh],
  );

  const handleHistoryStatusFiltersChange = useCallback(
    (stateFilters: CurtailmentEventState[]) => {
      void runAbortableRefresh((signal) => setHistoryStatusFilters(stateFilters, { signal })).catch(() => {});
    },
    [runAbortableRefresh, setHistoryStatusFilters],
  );

  const handleConfirmStop = useCallback(() => {
    if (!enableManage || !pendingStopConfirmation) {
      return;
    }

    const currentEvent = activeEvents.find((event) => event.id === pendingStopConfirmation.eventId);
    if (!currentEvent || !nonTerminalActiveEventStates.has(currentEvent.state)) {
      setPendingStopConfirmation(null);
      return;
    }

    const stopPromise = stopCurtailment(pendingStopConfirmation.eventId);

    void stopPromise.then(() => setPendingStopConfirmation(null)).catch(() => {});
  }, [activeEvents, enableManage, pendingStopConfirmation, stopCurtailment]);

  const handleConfirmTerminateRecovery = useCallback(
    (options: TerminateRecoveryOptions) => {
      if (!canUseRecovery || !pendingTerminateRecoveryEventId) {
        return;
      }

      if (
        pendingTerminateRecoveryEventId !== activeEventId ||
        !activeEvent ||
        !canTerminateRecoveryCurtailmentEvent(activeEvent)
      ) {
        setPendingTerminateRecoveryEventId(null);
        return;
      }

      void adminTerminateCurtailment(pendingTerminateRecoveryEventId, options)
        .then(() => setPendingTerminateRecoveryEventId(null))
        .catch(() => {});
    },
    [activeEvent, activeEventId, adminTerminateCurtailment, canUseRecovery, pendingTerminateRecoveryEventId],
  );

  const handleConfirmForceRelease = useCallback(
    (options: ForceReleaseCurtailmentOptions) => {
      if (!canUseRecovery || !pendingForceReleaseEventId) {
        return;
      }

      if (pendingForceReleaseEventId !== activeEventId || !activeEvent || !canAbortCurtailmentOwnership(activeEvent)) {
        setPendingForceReleaseEventId(null);
        return;
      }

      void forceReleaseCurtailment(pendingForceReleaseEventId, options)
        .then(() => setPendingForceReleaseEventId(null))
        .catch(() => {});
    },
    [activeEvent, activeEventId, canUseRecovery, forceReleaseCurtailment, pendingForceReleaseEventId],
  );

  const handleEditStopCurtailment = useCallback(() => {
    const editEventId = editSession?.eventId ?? activeEventId;

    closeModal();
    openStopConfirmation("stopCurtailment", editEventId);
  }, [activeEventId, closeModal, editSession, openStopConfirmation]);
  const handleEditSettings = useCallback(() => {
    navigate("/settings/curtailment");
  }, [navigate]);

  return (
    <section className={clsx("grid gap-6", className)}>
      <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header title="Curtailment" titleSize="text-heading-300" />
        {enableManage ? (
          <div className="flex items-center gap-2 phone:flex-col phone:items-stretch">
            <Button
              variant={variants.secondary}
              size={sizes.base}
              text="Edit settings"
              onClick={handleEditSettings}
              className="phone:w-full"
            />
            <Button
              variant={variants.primary}
              size={sizes.base}
              text="Run curtailment"
              onClick={openCreateModal}
              disabled={isStarting || isUpdating}
              className="phone:w-full"
            />
          </div>
        ) : null}
      </div>

      {errorMessage ? <CurtailmentMessage message={errorMessage} /> : null}

      {isInitialLoading ? (
        <div className="flex justify-center py-12">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <>
          {activeEvent ? (
            <ActiveCurtailmentStatus
              event={activeEvent}
              onDismissRestored={dismissTerminalCurtailment}
              onRequestTerminateRecovery={
                canUseRecovery &&
                canTerminateRecoveryCurtailmentEvent(activeEvent) &&
                !canAbortCurtailmentOwnership(activeEvent)
                  ? openTerminateRecoveryConfirmation
                  : undefined
              }
              onRequestForceRelease={
                canUseRecovery && activeEventId && canAbortCurtailmentOwnership(activeEvent)
                  ? () => setPendingForceReleaseEventId(activeEventId)
                  : undefined
              }
              onRequestEdit={enableManage ? openEditModal : undefined}
              onRequestRestore={enableManage ? () => openStopConfirmation("restore") : undefined}
              onRequestStop={
                enableManage && !activeEvent.isAutomationOwned
                  ? () => openStopConfirmation("stopCurtailment")
                  : undefined
              }
            />
          ) : null}

          <CurtailmentHistory
            activeEventId={activeEventId ?? undefined}
            activeEventIds={activeEventIds}
            events={historyEvents}
            pageSize={historyPageSize}
            currentPage={historyCurrentPage}
            hasNextPage={historyHasNextPage}
            hasPreviousPage={historyHasPreviousPage}
            selectedStatusFilters={historyStatusFilters}
            onPageChange={handleHistoryPageChange}
            onStatusFiltersChange={handleHistoryStatusFiltersChange}
            onManageActiveEvent={enableManage ? openHistoryManageModal : undefined}
            onSelectActiveEvent={canUseRecovery ? selectHistoryActiveEvent : undefined}
            onStopActiveEventRequested={enableManage ? cancelManageSelection : undefined}
            onStopActiveEvent={enableManage ? handleHistoryStop : undefined}
          />
        </>
      )}

      {modalMode ? (
        <CurtailmentStartModal
          open
          mode={modalMode}
          initialValues={isEditingCurtailment ? (editSession?.initialValues ?? undefined) : undefined}
          responseProfiles={isEditingCurtailment ? [] : responseProfileOptions}
          siteOptions={siteOptions}
          defaultSiteScope={isEditingCurtailment ? undefined : defaultSiteScope}
          siteScopeEnabled={siteOptions.length > 0 || isLoadingSiteOptions}
          isSiteScopeLoading={isLoadingSiteOptions}
          siteScopeDisabledReason={
            canReadSiteCatalog
              ? (siteOptionsLoadError ?? undefined)
              : "Site scope is not available for the current user."
          }
          preview={isEditingCurtailment ? editSession?.preview : undefined}
          onDismiss={closeModal}
          onSubmit={handleModalSubmit}
          onStopCurtailment={isEditingCurtailment ? handleEditStopCurtailment : undefined}
          isSubmitting={isModalSubmitting}
        />
      ) : null}

      {pendingStopConfirmation ? (
        <CurtailmentStopConfirmationDialog
          open
          action={pendingStopConfirmation.action}
          isSubmitting={isStopConfirmationSubmitting}
          onCancel={() => setPendingStopConfirmation(null)}
          onConfirm={handleConfirmStop}
        />
      ) : null}

      {pendingTerminateRecoveryEventId ? (
        <CurtailmentRecoveryTerminateDialog
          open
          error={adminTerminateError}
          isSubmitting={isTerminateRecoverySubmitting}
          onCancel={() => setPendingTerminateRecoveryEventId(null)}
          onConfirm={handleConfirmTerminateRecovery}
        />
      ) : null}

      {pendingForceReleaseEventId ? (
        <CurtailmentForceReleaseDialog
          open
          error={adminTerminateError}
          isSubmitting={isForceReleaseSubmitting}
          mode={pendingForceReleaseMode}
          onCancel={() => setPendingForceReleaseEventId(null)}
          onConfirm={handleConfirmForceRelease}
        />
      ) : null}
    </section>
  );
}

export default CurtailmentManagementPanel;
