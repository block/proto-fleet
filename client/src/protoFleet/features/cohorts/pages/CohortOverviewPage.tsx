import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import {
  type Cohort,
  type CohortDevice,
  CohortDeviceAssignment,
  CohortFirmwareRolloutState,
  type CohortFirmwareStatus,
  type CohortFirmwareTarget,
  type CohortMember,
  CohortState,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MeasurementType, type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import MinerSelectionList, { type MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import CohortActionsMenu from "@/protoFleet/features/cohorts/components/CohortActionsMenu";
import {
  cohortDeviceDisplayName,
  cohortDeviceSecondaryText,
  cohortStateLabel,
  durationToExpiresAt,
  type ExpiryPreset,
  type ExpiryUnit,
  firmwareRolloutStateLabel,
  formatCohortExpiryTimeLeft,
  formatCohortTimestamp,
  hasActiveFirmwareProgress,
  isActiveCohort,
  isActiveNonDefaultCohort,
  isSuperAdminRole,
} from "@/protoFleet/features/cohorts/utils";
import { EfficiencyPanel } from "@/protoFleet/features/dashboard/components/EfficiencyPanel";
import { HashratePanel } from "@/protoFleet/features/dashboard/components/HashratePanel";
import { PowerPanel } from "@/protoFleet/features/dashboard/components/PowerPanel";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { TemperaturePanel } from "@/protoFleet/features/dashboard/components/TemperaturePanel";
import { UptimePanel } from "@/protoFleet/features/dashboard/components/UptimePanel";
import { scopedPath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { useDuration, useRole, useSetDuration, useUsername } from "@/protoFleet/store";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { Alert, ChevronDown, Plus, Settings, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import { DatePickerField } from "@/shared/components/DatePicker";
import Dialog from "@/shared/components/Dialog";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Select, { type SelectOption } from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

type DeviceMutationMode = "add" | "remove" | "reassign";

type FirmwareTarget = {
  manufacturer: string;
  model: string;
};

type FirmwareTargetUpdate = FirmwareTarget & {
  firmwareFileId?: string;
};

type DetailValue = ReactNode | ReactNode[];
type ExtendMode = "duration" | "specific";
type AddMemberMode = "count" | "explicit";
type RefreshOptions = {
  showLoading?: boolean;
  suppressError?: boolean;
};

const extendPresetOptions = [
  { value: "4h", label: "4 hours" },
  { value: "8h", label: "8 hours" },
  { value: "24h", label: "24 hours" },
  { value: "3d", label: "3 days" },
  { value: "7d", label: "7 days" },
  { value: "custom", label: "Custom" },
];

const expiryUnitOptions = [
  { value: "hours", label: "Hours" },
  { value: "days", label: "Days" },
];

const extendModeSegments = [
  { key: "duration", title: "Duration" },
  { key: "specific", title: "Date/time" },
];

const addMemberModeOptions = [
  { value: "count", label: "Reserve count" },
  { value: "explicit", label: "Selected miners" },
];

const hourOptions = Array.from({ length: 24 }, (_, hour) => {
  const value = hour.toString().padStart(2, "0");
  return { value, label: value };
});

const minuteOptions = Array.from({ length: 12 }, (_, index) => {
  const value = (index * 5).toString().padStart(2, "0");
  return { value, label: value };
});

const cohortPerformanceMeasurementTypes = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
  MeasurementType.UPTIME,
];

const cohortFirmwareRefreshIntervalMs = Math.min(POLL_INTERVAL_MS, 3000);

const memberAddedAt = (member: CohortMember) =>
  member.addedAt ? new Date(timestampMs(member.addedAt)).toLocaleString() : "Unknown";

const parseCohortId = (value?: string) => {
  if (!value) return undefined;
  try {
    const parsed = BigInt(value);
    return parsed > 0n ? parsed : undefined;
  } catch {
    return undefined;
  }
};

const getCohortFirmwareTarget = (members: CohortMember[]) => {
  if (members.length === 0) return null;
  const first = members[0]?.display;
  const manufacturer = first?.manufacturer.trim();
  const model = first?.model.trim();
  if (!manufacturer || !model) return null;
  const normalizedManufacturer = manufacturer.toLocaleLowerCase();
  const normalizedModel = model.toLocaleLowerCase();
  return members.every((member) => {
    const display = member.display;
    return (
      display?.manufacturer.trim().toLocaleLowerCase() === normalizedManufacturer &&
      display?.model.trim().toLocaleLowerCase() === normalizedModel
    );
  })
    ? { manufacturer, model }
    : null;
};

const normalizeFirmwareTargetValue = (value: string) => value.trim().toLocaleLowerCase();

const firmwareTargetKey = (target: FirmwareTarget) =>
  `${normalizeFirmwareTargetValue(target.manufacturer)}:::${normalizeFirmwareTargetValue(target.model)}`;

const matchesFirmwareTarget = (file: FirmwareFileInfo, target: FirmwareTarget) =>
  normalizeFirmwareTargetValue(file.target_manufacturer) === normalizeFirmwareTargetValue(target.manufacturer) &&
  normalizeFirmwareTargetValue(file.target_model) === normalizeFirmwareTargetValue(target.model);

const getFirmwareFileIdForTarget = (targets: CohortFirmwareTarget[], target: FirmwareTarget | null) => {
  if (!target) return "";
  return (
    targets.find(
      (entry) =>
        normalizeFirmwareTargetValue(entry.manufacturer) === normalizeFirmwareTargetValue(target.manufacturer) &&
        normalizeFirmwareTargetValue(entry.model) === normalizeFirmwareTargetValue(target.model),
    )?.firmwareFileId ?? ""
  );
};

const formatCohortSource = (summary: NonNullable<Cohort["summary"]>) => {
  const sourceActorId = summary.sourceActorId.trim();
  if (sourceActorId.startsWith("device_set:")) return "Group";

  switch (summary.sourceActorType) {
    case "user":
      return summary.ownerUsername ? `User: ${summary.ownerUsername}` : "User";
    case "api_key":
      return "API key";
    case "scheduler":
      return "Scheduler";
    case "cohort":
      return "Cohort automation";
    default:
      return summary.sourceActorType || "Unknown";
  }
};

const formatBytes = (bytes: number) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  return `${value >= 10 || unitIndex === 0 ? Math.round(value) : value.toFixed(1)} ${units[unitIndex]}`;
};

const formatMinerCount = (count: number) => `${count} ${count === 1 ? "miner" : "miners"}`;

const formatFirmwareUploadedAt = (uploadedAt: string) => {
  const date = new Date(uploadedAt);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString();
};

const renderFirmwareFileSummary = (firmwareFiles: FirmwareFileInfo[], firmwareFileId: string) => {
  if (!firmwareFileId) return "None";
  const file = firmwareFiles.find((candidate) => candidate.id === firmwareFileId);
  if (!file) return "Unknown firmware file";

  const target = `${file.target_manufacturer} ${file.target_model}`.trim();
  const uploadedAt = formatFirmwareUploadedAt(file.uploaded_at);
  const size = formatBytes(file.size);

  return (
    <div className="min-w-0">
      <div className="font-medium break-words text-text-primary">{file.filename || "Firmware file"}</div>
      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-200 text-text-primary-70">
        {target ? <span>Target: {target}</span> : null}
        {uploadedAt ? <span>Uploaded: {uploadedAt}</span> : null}
        {size ? <span>Size: {size}</span> : null}
      </div>
    </div>
  );
};

const formatFirmwareTargetSummary = (cohort: Cohort, firmwareFiles: FirmwareFileInfo[]) => {
  const summary = cohort.summary;
  if (!summary) return "None";

  const configuredTargets = cohort.firmwareTargets.filter((target) => target.firmwareFileId);
  if (summary.isDefault) {
    if (configuredTargets.length === 0) return "None";
    if (configuredTargets.length === 1) {
      const [target] = configuredTargets;
      if (!target) return "None";
      return (
        <div className="flex flex-col gap-1">
          <div className="text-200 text-text-primary-70">
            {target.manufacturer} {target.model}
          </div>
          {renderFirmwareFileSummary(firmwareFiles, target.firmwareFileId)}
        </div>
      );
    }

    return (
      <div className="flex flex-col">
        {configuredTargets.map((target, index) => (
          <div
            key={firmwareTargetKey(target)}
            className={index === 0 ? "pb-3" : "border-t border-border-5 py-3 last:pb-0"}
          >
            <div className="mb-1 text-200 text-text-primary-70">
              {target.manufacturer} {target.model}
            </div>
            {renderFirmwareFileSummary(firmwareFiles, target.firmwareFileId)}
          </div>
        ))}
      </div>
    );
  }

  const firmwareFileId = configuredTargets[0]?.firmwareFileId || summary.desiredFirmwareFileId;
  return firmwareFileId ? renderFirmwareFileSummary(firmwareFiles, firmwareFileId) : "None";
};

const firmwareVersionLabel = (version?: string) => {
  const trimmed = version?.trim();
  return trimmed || "Unknown";
};

const cohortMemberSecondaryText = (member: CohortMember) => {
  const primary = cohortDeviceDisplayName(member);
  const secondary = cohortDeviceSecondaryText(member.display, primary);
  if (secondary) return secondary;
  return member.deviceIdentifier.trim().toLocaleLowerCase() === primary.trim().toLocaleLowerCase()
    ? ""
    : member.deviceIdentifier;
};

const CohortOverviewPage = () => {
  const { cohortId: cohortIdParam } = useParams<{ cohortId: string }>();
  const cohortId = useMemo(() => parseCohortId(cohortIdParam), [cohortIdParam]);
  const activeSite = useRouteSiteScope() ?? DEFAULT_ACTIVE_SITE;
  const navigate = useNavigate();
  const role = useRole();
  const username = useUsername();
  const isSuperAdmin = isSuperAdminRole(role);
  const { getCohort, extendCohort, setDesiredFirmware, addDevices, removeDevices, releaseCohort, adminReassign } =
    useCohortApi();
  const { listFirmwareFiles } = useFirmwareApi();

  const [cohort, setCohort] = useState<Cohort | null>(null);
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isMutating, setIsMutating] = useState(false);
  const [showExtendModal, setShowExtendModal] = useState(false);
  const [showFirmwareModal, setShowFirmwareModal] = useState(false);
  const [showReleaseDialog, setShowReleaseDialog] = useState(false);
  const [deviceMutationMode, setDeviceMutationMode] = useState<DeviceMutationMode | null>(null);

  const summary = cohort?.summary;
  const isOwnedByCurrentUser =
    summary?.ownerUsername.trim() !== "" &&
    username.trim() !== "" &&
    summary?.ownerUsername.trim().toLowerCase() === username.trim().toLowerCase();
  const canEditFirmware =
    isActiveCohort(summary) && (summary?.isDefault ? isSuperAdmin : isOwnedByCurrentUser || isSuperAdmin);
  const canMutate = isActiveNonDefaultCohort(summary) && (isOwnedByCurrentUser || isSuperAdmin);
  const firmwareTarget = useMemo(() => getCohortFirmwareTarget(cohort?.members ?? []), [cohort?.members]);
  const firmwareProgress = summary?.firmwareProgress;
  const shouldPollFirmware = isActiveCohort(summary) && hasActiveFirmwareProgress(firmwareProgress) && !isMutating;

  const loadFirmwareFiles = useCallback(async () => {
    try {
      const files = await listFirmwareFiles();
      setFirmwareFiles(files);
    } catch {
      setFirmwareFiles([]);
    }
  }, [listFirmwareFiles]);

  const refresh = useCallback(
    async ({ showLoading = true, suppressError = false }: RefreshOptions = {}) => {
      if (!cohortId) {
        setError("Invalid cohort id");
        setLoading(false);
        return;
      }
      if (showLoading) setLoading(true);
      if (!suppressError) setError(null);
      try {
        const next = await getCohort({ cohortId });
        setCohort(next);
      } catch {
        if (!suppressError) setError("Couldn't load cohort");
      } finally {
        if (showLoading) setLoading(false);
      }
    },
    [cohortId, getCohort],
  );

  useEffect(() => {
    queueMicrotask(() => void refresh());
  }, [refresh]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const files = await listFirmwareFiles();
        if (!cancelled) setFirmwareFiles(files);
      } catch {
        if (!cancelled) setFirmwareFiles([]);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [listFirmwareFiles]);

  useEffect(() => {
    if (!shouldPollFirmware) return undefined;

    const interval = window.setInterval(() => {
      void refresh({ showLoading: false, suppressError: true });
    }, cohortFirmwareRefreshIntervalMs);

    return () => window.clearInterval(interval);
  }, [refresh, shouldPollFirmware]);

  const handleExtend = useCallback(
    async (expiresAt: Date) => {
      if (!cohortId || !summary) return;
      setIsMutating(true);
      setError(null);
      try {
        const next = await extendCohort({ cohortId, expiresAt });
        setCohort(next);
        pushToast({ message: `Cohort "${summary.label}" extended`, status: STATUSES.success });
        setShowExtendModal(false);
      } catch {
        setError("Couldn't extend cohort");
      } finally {
        setIsMutating(false);
      }
    },
    [cohortId, extendCohort, summary],
  );

  const handleDeviceMutation = useCallback(
    async (mode: DeviceMutationMode, identifiers: string[]) => {
      if (!cohortId || !summary) return;
      setIsMutating(true);
      setError(null);
      try {
        const next =
          mode === "add"
            ? await addDevices({ cohortId, deviceIdentifiers: identifiers })
            : mode === "remove"
              ? await removeDevices({ cohortId, deviceIdentifiers: identifiers })
              : await adminReassign({ targetCohortId: cohortId, deviceIdentifiers: identifiers });
        setCohort(next);
        const verb = mode === "remove" ? "removed from" : "added to";
        pushToast({ message: `${identifiers.length} device(s) ${verb} "${summary.label}"`, status: STATUSES.success });
        setDeviceMutationMode(null);
      } catch {
        setError("Couldn't update cohort members");
      } finally {
        setIsMutating(false);
      }
    },
    [addDevices, adminReassign, cohortId, removeDevices, summary],
  );

  const handleFirmwareUpdate = useCallback(
    async ({ manufacturer, model, firmwareFileId }: FirmwareTargetUpdate) => {
      if (!cohortId || !summary) return;
      setIsMutating(true);
      setError(null);
      try {
        const next = await setDesiredFirmware({ cohortId, manufacturer, model, firmwareFileId });
        setCohort(next);
        void loadFirmwareFiles();
        const selectedFirmwareVersion = firmwareFiles
          .find((file) => file.id === firmwareFileId)
          ?.firmware_version?.trim();
        pushToast({
          message: firmwareFileId
            ? `Firmware ${selectedFirmwareVersion || "target"} saved for "${summary.label}". Rollout will begin on the next reconciler pass.`
            : `Firmware cleared for "${summary.label}"`,
          status: STATUSES.success,
        });
        setShowFirmwareModal(false);
      } catch {
        setError("Couldn't update cohort firmware");
      } finally {
        setIsMutating(false);
      }
    },
    [cohortId, firmwareFiles, loadFirmwareFiles, setDesiredFirmware, summary],
  );

  const handleDefaultFirmwareUpdate = useCallback(
    async (updates: FirmwareTargetUpdate[]) => {
      if (!cohortId || !summary) return;
      if (updates.length === 0) {
        setShowFirmwareModal(false);
        return;
      }

      setIsMutating(true);
      setError(null);
      try {
        let next: Cohort | null = null;
        for (const update of updates) {
          next = await setDesiredFirmware({ cohortId, ...update });
        }
        if (next) setCohort(next);
        void loadFirmwareFiles();
        pushToast({
          message: `${updates.length} firmware ${updates.length === 1 ? "target" : "targets"} saved for "${summary.label}". Rollout will begin on the next reconciler pass.`,
          status: STATUSES.success,
        });
        setShowFirmwareModal(false);
      } catch {
        setError("Couldn't update cohort firmware");
      } finally {
        setIsMutating(false);
      }
    },
    [cohortId, loadFirmwareFiles, setDesiredFirmware, summary],
  );

  const handleRelease = useCallback(async () => {
    if (!cohortId || !summary) return;
    setIsMutating(true);
    setError(null);
    try {
      const next = await releaseCohort({ cohortId });
      setCohort(next);
      pushToast({ message: `Cohort "${summary.label}" released`, status: STATUSES.success });
      setShowReleaseDialog(false);
    } catch {
      setError("Couldn't release cohort");
    } finally {
      setIsMutating(false);
    }
  }, [cohortId, releaseCohort, summary]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (error && !cohort) {
    return (
      <div className="p-6 laptop:p-10">
        <Header
          title="Cohort unavailable"
          titleSize="text-heading-300"
          icon={<ChevronDown className="rotate-90" />}
          iconAriaLabel="Back to cohorts"
          iconOnClick={() => navigate(scopedPath("/cohorts", activeSite))}
        />
        <div className="mt-4">
          <Callout intent="danger" prefixIcon={<Alert />} title={error} />
        </div>
      </div>
    );
  }

  if (!summary) {
    return (
      <div className="p-6 laptop:p-10">
        <Header
          title="Cohort not found"
          titleSize="text-heading-300"
          icon={<ChevronDown className="rotate-90" />}
          iconAriaLabel="Back to cohorts"
          iconOnClick={() => navigate(scopedPath("/cohorts", activeSite))}
        />
      </div>
    );
  }

  const expiryTimeLeft =
    !summary.isDefault && summary.state === CohortState.ACTIVE
      ? formatCohortExpiryTimeLeft(summary.expiresAt)
      : undefined;

  return (
    <div className="flex flex-col gap-6 p-6 laptop:p-10" data-testid="cohort-overview-page">
      <Header
        title={summary.label}
        titleSize="text-heading-300"
        inline
        icon={<ChevronDown className="rotate-90" />}
        iconAriaLabel="Back to cohorts"
        iconOnClick={() => navigate(scopedPath("/cohorts", activeSite))}
      >
        <div className="ml-3 flex shrink-0 items-center gap-2">
          {summary.isDefault ? (
            <Button
              text="Default firmware"
              size={sizes.compact}
              variant={variants.secondary}
              prefixIcon={<Settings />}
              disabled={!canEditFirmware || isMutating}
              onClick={() => setShowFirmwareModal(true)}
            />
          ) : (
            <>
              <Button
                text="Add"
                size={sizes.compact}
                variant={variants.secondary}
                prefixIcon={<Plus />}
                disabled={!canMutate || isMutating}
                onClick={() => setDeviceMutationMode("add")}
              />
              <Button
                text="Remove"
                size={sizes.compact}
                variant={variants.secondary}
                prefixIcon={<Trash />}
                disabled={!canMutate || isMutating || cohort.members.length === 0}
                onClick={() => setDeviceMutationMode("remove")}
              />
              <CohortActionsMenu
                disabled={!isActiveCohort(summary) || isMutating}
                firmwareDisabled={!canEditFirmware || isMutating}
                mutationDisabled={!canMutate || isMutating}
                isSuperAdmin={isSuperAdmin}
                onFirmware={() => setShowFirmwareModal(true)}
                onExtend={() => setShowExtendModal(true)}
                onRelease={() => setShowReleaseDialog(true)}
                onAdminReassign={() => setDeviceMutationMode("reassign")}
              />
            </>
          )}
        </div>
      </Header>

      {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}

      <section className="grid gap-4 tablet:grid-cols-2 desktop:grid-cols-4">
        <OverviewMetric label="State" value={cohortStateLabel(summary.state)} />
        <OverviewMetric label="Members" value={summary.explicitMemberCount.toString()} />
        <OverviewMetric label="Owner" value={summary.ownerUsername || "Unowned"} />
        <OverviewMetric
          label="Expires"
          value={
            <>
              {formatCohortTimestamp(summary.expiresAt)}
              {expiryTimeLeft ? <span className="text-text-primary-50"> · {expiryTimeLeft}</span> : null}
            </>
          }
        />
      </section>

      {!summary.isDefault ? <CohortPerformanceSection members={cohort.members} /> : null}

      <section className="overflow-hidden rounded-lg border border-border-5">
        <div className="border-b border-border-5 px-4 py-3">
          <Header title="Members" titleSize="text-heading-100" />
        </div>
        <div className="overflow-x-auto">
          <table className="w-full table-fixed text-left text-300">
            <thead className="bg-surface-raised text-text-primary-70">
              <tr>
                <th className="w-[32%] px-4 py-3 font-medium">Miner</th>
                <th className="w-[18%] px-4 py-3 font-medium">Firmware</th>
                <th className="w-[24%] px-4 py-3 font-medium">Rollout</th>
                <th className="w-[26%] px-4 py-3 font-medium">Added</th>
              </tr>
            </thead>
            <tbody>
              {cohort.members.map((member) => {
                const memberName = cohortDeviceDisplayName(member);
                const memberSecondary = cohortMemberSecondaryText(member);

                return (
                  <tr key={member.deviceIdentifier} className="border-t border-border-5">
                    <td className="px-4 py-3">
                      <div className="truncate font-medium" title={member.deviceIdentifier}>
                        {memberName}
                      </div>
                      {memberSecondary ? (
                        <div className="truncate text-200 text-text-primary-70">{memberSecondary}</div>
                      ) : null}
                    </td>
                    <td className="px-4 py-3">
                      <MemberFirmwareCell
                        status={member.firmwareStatus}
                        fallbackCurrentVersion={member.display?.firmwareVersion}
                      />
                    </td>
                    <td className="px-4 py-3">
                      <MemberRolloutCell status={member.firmwareStatus} />
                    </td>
                    <td className="px-4 py-3">{memberAddedAt(member)}</td>
                  </tr>
                );
              })}
              {cohort.members.length === 0 ? (
                <tr>
                  <td className="px-4 py-8 text-text-primary-70" colSpan={4}>
                    No explicit members.
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>

      <section className="grid gap-4 desktop:grid-cols-2">
        <DetailBlock label="Purpose" value={summary.purpose || "Reservation"} />
        <DetailBlock label="Source" value={formatCohortSource(summary)} />
        <DetailBlock label="Firmware" value={formatFirmwareTargetSummary(cohort, firmwareFiles)} />
      </section>

      {showExtendModal ? (
        <ExtendModal
          currentExpiresAt={summary.expiresAt ? new Date(timestampMs(summary.expiresAt)) : undefined}
          isSubmitting={isMutating}
          onDismiss={() => setShowExtendModal(false)}
          onSubmit={handleExtend}
        />
      ) : null}

      {showFirmwareModal && summary.isDefault ? (
        <DefaultFirmwareModal
          cohort={cohort}
          isSubmitting={isMutating}
          onDismiss={() => setShowFirmwareModal(false)}
          onSubmit={handleDefaultFirmwareUpdate}
        />
      ) : null}

      {showFirmwareModal && !summary.isDefault ? (
        <FirmwareModal
          initialFirmwareFileId={
            getFirmwareFileIdForTarget(cohort.firmwareTargets, firmwareTarget) || summary.desiredFirmwareFileId
          }
          target={firmwareTarget}
          memberCount={cohort.members.length}
          isSubmitting={isMutating}
          onDismiss={() => setShowFirmwareModal(false)}
          onSubmit={handleFirmwareUpdate}
        />
      ) : null}

      {deviceMutationMode ? (
        <DeviceMutationModal
          mode={deviceMutationMode}
          members={cohort.members}
          target={firmwareTarget}
          isSubmitting={isMutating}
          onDismiss={() => setDeviceMutationMode(null)}
          onSubmit={(identifiers) => handleDeviceMutation(deviceMutationMode, identifiers)}
        />
      ) : null}

      {showReleaseDialog ? (
        <Dialog
          title={`Release "${summary.label}"?`}
          subtitle="Devices in this cohort will return to the default cohort."
          onDismiss={() => setShowReleaseDialog(false)}
          buttons={[
            { text: "Cancel", onClick: () => setShowReleaseDialog(false), variant: variants.secondary },
            { text: "Release", onClick: handleRelease, variant: variants.danger, loading: isMutating },
          ]}
        />
      ) : null}
    </div>
  );
};

const metricsByType = (metrics: Metric[] | undefined, measurementType: MeasurementType) =>
  metrics?.filter((metric) => metric.measurementType === measurementType);

const CohortPerformanceSection = ({ members }: { members: CohortMember[] }) => {
  const duration = useDuration();
  const setDuration = useSetDuration();
  const deviceIds = useMemo(() => members.map((member) => member.deviceIdentifier), [members]);
  const hasMembers = deviceIds.length > 0;

  const { data: telemetryData, error: telemetryError } = useTelemetryMetrics({
    deviceIds,
    measurementTypes: cohortPerformanceMeasurementTypes,
    duration,
    enabled: hasMembers,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  const allMetrics = useMemo(
    () => (hasMembers ? (telemetryData?.metrics ?? []) : []),
    [hasMembers, telemetryData?.metrics],
  );
  const hashrateMetrics = useMemo(() => metricsByType(allMetrics, MeasurementType.HASHRATE), [allMetrics]);
  const powerMetrics = useMemo(() => metricsByType(allMetrics, MeasurementType.POWER), [allMetrics]);
  const efficiencyMetrics = useMemo(() => metricsByType(allMetrics, MeasurementType.EFFICIENCY), [allMetrics]);
  const temperatureStatusCounts = hasMembers ? telemetryData?.temperatureStatusCounts : [];
  const uptimeStatusCounts = hasMembers ? telemetryData?.uptimeStatusCounts : [];

  return (
    <section data-testid="cohort-performance-section">
      <SectionHeading heading="Performance">
        <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
      </SectionHeading>
      {telemetryError ? (
        <div className="mt-4">
          <Callout intent="danger" prefixIcon={<Alert />} title="Couldn't load cohort performance" />
        </div>
      ) : null}
      <div className="mt-4 flex flex-col gap-1">
        <HashratePanel duration={duration} metrics={hashrateMetrics} />
        <UptimePanel duration={duration} uptimeStatusCounts={uptimeStatusCounts} />
        <TemperaturePanel duration={duration} temperatureStatusCounts={temperatureStatusCounts} />
        <div className="grid grid-cols-1 gap-1 laptop:grid-cols-2">
          <PowerPanel duration={duration} metrics={powerMetrics} totalMiners={members.length} />
          <EfficiencyPanel duration={duration} metrics={efficiencyMetrics} totalMiners={members.length} />
        </div>
      </div>
    </section>
  );
};

const OverviewMetric = ({ label, value }: { label: string; value: ReactNode }) => (
  <div className="rounded-lg border border-border-5 bg-surface-base p-4">
    <div className="text-200 text-text-primary-70">{label}</div>
    <div className="mt-1 truncate text-heading-100 text-text-primary">{value}</div>
  </div>
);

const firmwareStateTextClass = (state?: CohortFirmwareRolloutState) => {
  switch (state) {
    case CohortFirmwareRolloutState.COMPLETE:
      return "text-intent-success-fill";
    case CohortFirmwareRolloutState.NEEDS_ATTENTION:
      return "text-intent-critical-fill";
    case CohortFirmwareRolloutState.UPDATING:
    case CohortFirmwareRolloutState.QUEUED:
    case CohortFirmwareRolloutState.VERIFYING:
      return "text-core-primary-fill";
    default:
      return "text-text-primary-70";
  }
};

const formatFirmwareTimestamp = (timestamp?: CohortFirmwareStatus["observedAt"]) =>
  timestamp ? new Date(timestampMs(timestamp)).toLocaleString() : "";

const firmwareStatusTimeLabel = (status?: CohortFirmwareStatus) => {
  const confirmed = formatFirmwareTimestamp(status?.confirmedAt);
  if (confirmed) return `Confirmed ${confirmed}`;
  const observed = formatFirmwareTimestamp(status?.observedAt);
  if (observed) return `Updated ${observed}`;
  const dispatched = formatFirmwareTimestamp(status?.lastDispatchedAt);
  return dispatched ? `Dispatched ${dispatched}` : "";
};

const MemberFirmwareCell = ({
  status,
  fallbackCurrentVersion,
}: {
  status?: CohortFirmwareStatus;
  fallbackCurrentVersion?: string;
}) => {
  const current = firmwareVersionLabel(status?.currentFirmwareVersion || fallbackCurrentVersion);
  const target = status?.targetFirmwareVersion.trim();
  const hasTarget = Boolean(status?.targetFirmwareFileId);

  return (
    <div className="min-w-0">
      <div className="truncate font-medium text-text-primary" title={current}>
        {current}
      </div>
      {hasTarget ? (
        <div className="mt-1 truncate text-200 text-text-primary-70" title={target || "Target: Unknown"}>
          Target: {target || "Unknown"}
        </div>
      ) : null}
    </div>
  );
};

const MemberRolloutCell = ({ status }: { status?: CohortFirmwareStatus }) => {
  const hasTarget = Boolean(status?.targetFirmwareFileId);
  const stateLabel = status && hasTarget ? firmwareRolloutStateLabel(status.state) : "No target";
  const eventTime = firmwareStatusTimeLabel(status);
  const lastError = status?.lastError.trim();

  return (
    <div className="min-w-0">
      <div className={`truncate font-medium ${firmwareStateTextClass(status?.state)}`} title={stateLabel}>
        {stateLabel}
      </div>
      {eventTime ? (
        <div className="mt-1 truncate text-200 text-text-primary-50" title={eventTime}>
          {eventTime}
        </div>
      ) : null}
      {status && status.retryCount > 0 ? (
        <div className="mt-1 truncate text-200 text-text-primary-50">
          {status.retryCount} {status.retryCount === 1 ? "retry" : "retries"}
        </div>
      ) : null}
      {lastError ? (
        <div className="mt-1 truncate text-200 text-intent-critical-fill" title={lastError}>
          {lastError}
        </div>
      ) : null}
    </div>
  );
};

const DetailBlock = ({ label, value }: { label: string; value: DetailValue }) => (
  <div className="rounded-lg border border-border-5 bg-surface-base p-4">
    <div className="text-200 text-text-primary-70">{label}</div>
    {Array.isArray(value) ? (
      <div className="mt-1 flex flex-col gap-1 text-300 break-words text-text-primary">
        {value.map((item, index) => (
          <div key={index}>{item}</div>
        ))}
      </div>
    ) : (
      <div className="mt-1 text-300 break-words text-text-primary">{value}</div>
    )}
  </div>
);

interface ExtendModalProps {
  currentExpiresAt?: Date;
  isSubmitting: boolean;
  onDismiss: () => void;
  onSubmit: (expiresAt: Date) => void;
}

const getExtendBaseDate = (currentExpiresAt?: Date) => {
  const now = new Date();
  return currentExpiresAt && currentExpiresAt.getTime() > now.getTime() ? currentExpiresAt : now;
};

const roundToFiveMinutes = (date: Date) => {
  const next = new Date(date);
  const remainder = next.getMinutes() % 5;
  if (remainder !== 0) {
    next.setMinutes(next.getMinutes() + (5 - remainder));
  }
  next.setSeconds(0, 0);
  return next;
};

const combineDateAndTime = (date: Date | undefined, hour: string, minute: string) => {
  if (!date) return undefined;
  const next = new Date(date);
  next.setHours(Number.parseInt(hour, 10), Number.parseInt(minute, 10), 0, 0);
  return next;
};

const isPastDate = (date: Date) => {
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  return date < today;
};

const ExtendModal = ({ currentExpiresAt, isSubmitting, onDismiss, onSubmit }: ExtendModalProps) => {
  const baseDate = useMemo(() => getExtendBaseDate(currentExpiresAt), [currentExpiresAt]);
  const initialSpecificDate = useMemo(() => roundToFiveMinutes(baseDate), [baseDate]);
  const [extendMode, setExtendMode] = useState<ExtendMode>("duration");
  const [expiryPreset, setExpiryPreset] = useState<ExpiryPreset>("24h");
  const [customExpiryAmount, setCustomExpiryAmount] = useState("1");
  const [customExpiryUnit, setCustomExpiryUnit] = useState<ExpiryUnit>("days");
  const [specificDate, setSpecificDate] = useState(initialSpecificDate);
  const [specificHour, setSpecificHour] = useState(initialSpecificDate.getHours().toString().padStart(2, "0"));
  const [specificMinute, setSpecificMinute] = useState(initialSpecificDate.getMinutes().toString().padStart(2, "0"));
  const [error, setError] = useState("");

  const durationExpiresAt = useMemo(() => {
    try {
      return durationToExpiresAt(expiryPreset, customExpiryAmount, customExpiryUnit, baseDate);
    } catch {
      return undefined;
    }
  }, [baseDate, customExpiryAmount, customExpiryUnit, expiryPreset]);

  const specificExpiresAt = useMemo(
    () => combineDateAndTime(specificDate, specificHour, specificMinute),
    [specificDate, specificHour, specificMinute],
  );

  const selectedExpiresAt = extendMode === "duration" ? durationExpiresAt : specificExpiresAt;
  const selectedExpiresAtLabel = selectedExpiresAt ? selectedExpiresAt.toLocaleString() : "Select an expiry";

  const handleSubmit = useCallback(() => {
    if (!selectedExpiresAt) {
      setError("Expiration is required");
      return;
    }
    if (selectedExpiresAt.getTime() <= Date.now()) {
      setError("Expiration must be in the future");
      return;
    }
    onSubmit(selectedExpiresAt);
  }, [onSubmit, selectedExpiresAt]);

  return (
    <Modal
      open
      title="Extend cohort"
      onDismiss={onDismiss}
      buttons={[
        {
          text: "Extend",
          variant: variants.primary,
          onClick: handleSubmit,
          loading: isSubmitting,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
      <div className="mt-4 flex flex-col gap-4">
        {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}
        <SegmentedControl
          segments={extendModeSegments}
          initialSegmentKey={extendMode}
          onSelect={(selectedKey) => {
            setExtendMode(selectedKey as ExtendMode);
            setError("");
          }}
        />

        {extendMode === "duration" ? (
          <>
            <Select
              id="cohort-extend-expiry-preset"
              label="Extend by"
              options={extendPresetOptions}
              value={expiryPreset}
              onChange={(value) => {
                setExpiryPreset(value as ExpiryPreset);
                setError("");
              }}
              forceBelow
            />
            {expiryPreset === "custom" ? (
              <div className="grid gap-4 tablet:grid-cols-2">
                <Input
                  id="cohort-extend-custom-expiry-amount"
                  label="Duration"
                  initValue={customExpiryAmount}
                  onChange={(value) => {
                    setCustomExpiryAmount(value);
                    setError("");
                  }}
                  inputMode="decimal"
                  type="number"
                  required
                />
                <Select
                  id="cohort-extend-custom-expiry-unit"
                  label="Unit"
                  options={expiryUnitOptions}
                  value={customExpiryUnit}
                  onChange={(value) => {
                    setCustomExpiryUnit(value as ExpiryUnit);
                    setError("");
                  }}
                  forceBelow
                />
              </div>
            ) : null}
          </>
        ) : (
          <>
            <DatePickerField
              id="cohort-extend-specific-date"
              label="Date"
              labelPlacement="floating"
              selectedDate={specificDate}
              onSelectedDateChange={(date) => {
                setSpecificDate(date);
                setError("");
              }}
              isDateDisabled={isPastDate}
              popoverRenderMode="portal-scrolling"
              testId="cohort-extend-specific-date"
            />
            <div className="grid gap-4 tablet:grid-cols-2">
              <Select
                id="cohort-extend-specific-hour"
                label="Hour"
                options={hourOptions}
                value={specificHour}
                onChange={(value) => {
                  setSpecificHour(value);
                  setError("");
                }}
                forceBelow
              />
              <Select
                id="cohort-extend-specific-minute"
                label="Minute"
                options={minuteOptions}
                value={specificMinute}
                onChange={(value) => {
                  setSpecificMinute(value);
                  setError("");
                }}
                forceBelow
              />
            </div>
          </>
        )}

        <div className="rounded-lg bg-core-primary-5 px-4 py-3">
          <div className="text-200 text-text-primary-70">New expiry</div>
          <div className="mt-1 text-emphasis-300 text-text-primary">{selectedExpiresAtLabel}</div>
        </div>
      </div>
    </Modal>
  );
};

interface FirmwareModalProps {
  initialFirmwareFileId: string;
  target: FirmwareTarget | null;
  memberCount: number;
  isSubmitting: boolean;
  onDismiss: () => void;
  onSubmit: (update: FirmwareTargetUpdate) => void;
}

const formatFirmwareOption = (file: FirmwareFileInfo) => ({
  value: file.id,
  label: file.filename || file.id,
  description: `${file.target_manufacturer} ${file.target_model}`.trim() || file.id,
});

const FirmwareModal = ({
  initialFirmwareFileId,
  target,
  memberCount,
  isSubmitting,
  onDismiss,
  onSubmit,
}: FirmwareModalProps) => {
  const { listFirmwareFiles } = useFirmwareApi();
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [selectedFirmwareFileId, setSelectedFirmwareFileId] = useState(initialFirmwareFileId);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    listFirmwareFiles()
      .then((files) => {
        if (!cancelled) setFirmwareFiles(files);
      })
      .catch((loadError) => {
        if (!cancelled) {
          setError(loadError?.message || "Couldn't load firmware files");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [listFirmwareFiles]);

  const compatibleFirmwareFiles = useMemo(
    () => (target ? firmwareFiles.filter((file) => matchesFirmwareTarget(file, target)) : []),
    [firmwareFiles, target],
  );

  const firmwareOptions = useMemo(() => {
    const options = [{ value: "", label: "No firmware" }, ...compatibleFirmwareFiles.map(formatFirmwareOption)];
    if (initialFirmwareFileId && !options.some((option) => option.value === initialFirmwareFileId)) {
      options.push({ value: initialFirmwareFileId, label: initialFirmwareFileId });
    }
    return options;
  }, [compatibleFirmwareFiles, initialFirmwareFileId]);
  const selectedFirmwareFile = useMemo(
    () => firmwareFiles.find((file) => file.id === selectedFirmwareFileId),
    [firmwareFiles, selectedFirmwareFileId],
  );
  const selectedFirmwareVersion = selectedFirmwareFile?.firmware_version?.trim();

  return (
    <Modal
      open
      title="Set firmware"
      onDismiss={onDismiss}
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: () => {
            if (target) onSubmit({ ...target, firmwareFileId: selectedFirmwareFileId || undefined });
          },
          loading: isSubmitting,
          disabled: !target,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
      <div className="mt-4 flex flex-col gap-4">
        {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}
        {!target ? (
          <Callout intent="danger" prefixIcon={<Alert />} title="Firmware requires a single product and model." />
        ) : null}
        <Select
          id="cohort-desired-firmware-file-id"
          label="Firmware"
          options={firmwareOptions}
          value={selectedFirmwareFileId}
          onChange={(value) => {
            setSelectedFirmwareFileId(value);
            setError("");
          }}
          disabled={!target}
          forceBelow
        />
        {target && selectedFirmwareFileId ? (
          <div className="rounded-lg bg-core-primary-5 px-4 py-3 text-300 text-text-primary">
            <div className="text-200 text-text-primary-70">Rollout target</div>
            <div className="mt-1">
              {selectedFirmwareVersion || "Unknown version"} · {formatMinerCount(memberCount)}
            </div>
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

interface DefaultFirmwareModalProps {
  cohort: Cohort;
  isSubmitting: boolean;
  onDismiss: () => void;
  onSubmit: (updates: FirmwareTargetUpdate[]) => void;
}

const getDefaultFirmwareTargets = (modelGroups: MinerModelGroup[], configuredTargets: CohortFirmwareTarget[]) => {
  const byKey = new Map<string, FirmwareTarget>();

  for (const group of modelGroups) {
    const manufacturer = group.manufacturer.trim();
    const model = group.model.trim();
    if (!manufacturer || !model) continue;
    const target = { manufacturer, model };
    byKey.set(firmwareTargetKey(target), target);
  }

  for (const configuredTarget of configuredTargets) {
    const manufacturer = configuredTarget.manufacturer.trim();
    const model = configuredTarget.model.trim();
    if (!manufacturer || !model) continue;
    const target = { manufacturer, model };
    byKey.set(firmwareTargetKey(target), target);
  }

  return [...byKey.values()].sort(
    (a, b) => a.manufacturer.localeCompare(b.manufacturer) || a.model.localeCompare(b.model),
  );
};

const selectedFirmwareMap = (targets: FirmwareTarget[], configuredTargets: CohortFirmwareTarget[]) => {
  const configuredByKey = new Map(
    configuredTargets.map((target) => [firmwareTargetKey(target), target.firmwareFileId] as const),
  );
  return Object.fromEntries(
    targets.map((target) => [firmwareTargetKey(target), configuredByKey.get(firmwareTargetKey(target)) ?? ""]),
  );
};

const firmwareOptionsForTarget = (
  target: FirmwareTarget,
  firmwareFiles: FirmwareFileInfo[],
  selectedFirmwareFileId: string,
): SelectOption[] => {
  const compatibleOptions = firmwareFiles
    .filter((file) => matchesFirmwareTarget(file, target))
    .map(formatFirmwareOption);
  const options = [{ value: "", label: "No firmware" }, ...compatibleOptions];
  if (selectedFirmwareFileId && !options.some((option) => option.value === selectedFirmwareFileId)) {
    options.push({ value: selectedFirmwareFileId, label: selectedFirmwareFileId });
  }
  return options;
};

const minerCountForTarget = (target: FirmwareTarget, modelGroups: MinerModelGroup[]) =>
  modelGroups.find(
    (group) =>
      normalizeFirmwareTargetValue(group.manufacturer) === normalizeFirmwareTargetValue(target.manufacturer) &&
      normalizeFirmwareTargetValue(group.model) === normalizeFirmwareTargetValue(target.model),
  )?.count ?? 0;

const DefaultFirmwareModal = ({ cohort, isSubmitting, onDismiss, onSubmit }: DefaultFirmwareModalProps) => {
  const { listFirmwareFiles } = useFirmwareApi();
  const { getMinerModelGroups } = useMinerModelGroups();
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [modelGroups, setModelGroups] = useState<MinerModelGroup[]>([]);
  const [selectedFirmwareOverrides, setSelectedFirmwareOverrides] = useState<Record<string, string>>({});
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    listFirmwareFiles()
      .then((files) => {
        if (!cancelled) setFirmwareFiles(files);
      })
      .catch((loadError) => {
        if (!cancelled) setError(loadError?.message || "Couldn't load firmware files");
      });

    void getMinerModelGroups(null)
      .then((groups) => {
        if (!cancelled) setModelGroups(groups);
      })
      .catch((loadError) => {
        if (!cancelled) setError(loadError?.message || "Couldn't load miner models");
      });

    return () => {
      cancelled = true;
    };
  }, [getMinerModelGroups, listFirmwareFiles]);

  const firmwareTargets = useMemo(
    () => getDefaultFirmwareTargets(modelGroups, cohort.firmwareTargets),
    [cohort.firmwareTargets, modelGroups],
  );

  const initialSelectedFirmwareByTarget = useMemo(
    () => selectedFirmwareMap(firmwareTargets, cohort.firmwareTargets),
    [cohort.firmwareTargets, firmwareTargets],
  );

  const handleSubmit = useCallback(() => {
    const updates: FirmwareTargetUpdate[] = [];
    for (const target of firmwareTargets) {
      const key = firmwareTargetKey(target);
      const initialFirmwareFileId = initialSelectedFirmwareByTarget[key] ?? "";
      const selectedFirmwareFileId = selectedFirmwareOverrides[key] ?? initialFirmwareFileId;
      if (initialFirmwareFileId === selectedFirmwareFileId) continue;

      const update: FirmwareTargetUpdate = { ...target };
      if (selectedFirmwareFileId) update.firmwareFileId = selectedFirmwareFileId;
      updates.push(update);
    }

    onSubmit(updates);
  }, [firmwareTargets, initialSelectedFirmwareByTarget, onSubmit, selectedFirmwareOverrides]);

  return (
    <Modal
      open
      title="Set default firmware"
      onDismiss={onDismiss}
      size="large"
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: handleSubmit,
          loading: isSubmitting,
          disabled: firmwareTargets.length === 0,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
      <div className="mt-4 flex flex-col gap-4">
        {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}
        {firmwareTargets.length === 0 ? (
          <Callout
            intent="danger"
            prefixIcon={<Alert />}
            title="No fleet product and model combinations are available."
          />
        ) : null}
        <div className="overflow-hidden rounded-lg border border-border-5">
          <table className="w-full table-fixed text-left text-300">
            <thead className="bg-surface-raised text-text-primary-70">
              <tr>
                <th className="w-[36%] px-4 py-3 font-medium">Product</th>
                <th className="w-[34%] px-4 py-3 font-medium">Model</th>
                <th className="w-[30%] px-4 py-3 font-medium">Firmware</th>
              </tr>
            </thead>
            <tbody>
              {firmwareTargets.map((target, index) => {
                const key = firmwareTargetKey(target);
                const selectedFirmwareFileId =
                  selectedFirmwareOverrides[key] ?? initialSelectedFirmwareByTarget[key] ?? "";
                const selectedFirmwareFile = firmwareFiles.find((file) => file.id === selectedFirmwareFileId);
                const selectedFirmwareVersion = selectedFirmwareFile?.firmware_version?.trim();
                const targetMinerCount = minerCountForTarget(target, modelGroups);
                return (
                  <tr key={key} className="border-t border-border-5">
                    <td className="px-4 py-3">
                      <div className="truncate" title={target.manufacturer}>
                        {target.manufacturer}
                      </div>
                      <div className="mt-1 truncate text-200 text-text-primary-70">
                        {formatMinerCount(targetMinerCount)}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="truncate" title={target.model}>
                        {target.model}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <Select
                        id={`default-cohort-firmware-${index}`}
                        label="Firmware"
                        options={firmwareOptionsForTarget(target, firmwareFiles, selectedFirmwareFileId)}
                        value={selectedFirmwareFileId}
                        onChange={(value) => {
                          setSelectedFirmwareOverrides((current) => ({ ...current, [key]: value }));
                          setError("");
                        }}
                        forceBelow
                      />
                      {selectedFirmwareFileId ? (
                        <div className="mt-2 truncate text-200 text-text-primary-70">
                          Target version: {selectedFirmwareVersion || "Unknown"}
                        </div>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </Modal>
  );
};

interface DeviceMutationModalProps {
  mode: DeviceMutationMode;
  members: CohortMember[];
  target?: FirmwareTarget | null;
  isSubmitting: boolean;
  onDismiss: () => void;
  onSubmit: (identifiers: string[]) => void;
}

const mutationTitle: Record<DeviceMutationMode, string> = {
  add: "Add members",
  remove: "Remove members",
  reassign: "Admin reassign",
};

const mutationButton: Record<DeviceMutationMode, string> = {
  add: "Add",
  remove: "Remove",
  reassign: "Reassign",
};

const DeviceMutationModal = ({
  mode,
  members,
  target,
  isSubmitting,
  onDismiss,
  onSubmit,
}: DeviceMutationModalProps) => {
  const { listAllDevices } = useCohortApi();
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [selectedMemberIds, setSelectedMemberIds] = useState<Set<string>>(() => new Set());
  const [error, setError] = useState("");
  const [addMode, setAddMode] = useState<AddMemberMode>("count");
  const [count, setCount] = useState("1");
  const [eligibleDevices, setEligibleDevices] = useState<CohortDevice[]>([]);
  const [isLoadingEligibleDevices, setIsLoadingEligibleDevices] = useState(false);
  const isRemove = mode === "remove";
  const isAdd = mode === "add";
  const isCompactModal = isRemove || (isAdd && addMode === "count");
  const selectedRemoveCount = selectedMemberIds.size;
  const fixedModelFilter = useMemo(() => (target?.model ? [target.model] : []), [target]);
  const eligibleDeviceIds = useMemo(
    () => new Set(eligibleDevices.map((device) => device.deviceIdentifier)),
    [eligibleDevices],
  );

  useEffect(() => {
    if (!isAdd || !target) return;

    let cancelled = false;
    queueMicrotask(() => {
      if (!cancelled) setIsLoadingEligibleDevices(true);
    });
    void listAllDevices({
      filter: {
        assignments: [CohortDeviceAssignment.AVAILABLE],
        manufacturers: [target.manufacturer],
        models: [target.model],
      },
    })
      .then((devices) => {
        if (!cancelled) setEligibleDevices(devices);
      })
      .catch((loadError) => {
        if (!cancelled) {
          setEligibleDevices([]);
          setError(loadError?.message || "Couldn't load eligible miners");
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoadingEligibleDevices(false);
      });

    return () => {
      cancelled = true;
    };
  }, [isAdd, listAllDevices, target]);

  const toggleMember = useCallback((deviceIdentifier: string) => {
    setSelectedMemberIds((current) => {
      const next = new Set(current);
      if (next.has(deviceIdentifier)) {
        next.delete(deviceIdentifier);
      } else {
        next.add(deviceIdentifier);
      }
      return next;
    });
    setError("");
  }, []);

  const handleSubmit = useCallback(() => {
    if (isAdd && !target) {
      setError("Adding members requires a single product and model.");
      return;
    }
    if (isAdd && addMode === "count") {
      const parsedCount = Number.parseInt(count, 10);
      if (!Number.isFinite(parsedCount) || parsedCount <= 0 || parsedCount > 10000) {
        setError("Count must be between 1 and 10000");
        return;
      }
      if (eligibleDevices.length < parsedCount) {
        setError(
          `Only ${eligibleDevices.length} eligible ${eligibleDevices.length === 1 ? "miner is" : "miners are"} available.`,
        );
        return;
      }
      onSubmit(eligibleDevices.slice(0, parsedCount).map((device) => device.deviceIdentifier));
      return;
    }

    const identifiers = isRemove
      ? Array.from(selectedMemberIds)
      : (selectionRef.current?.getSelection().selectedItems ?? []);
    if (identifiers.length === 0) {
      setError(isRemove ? "Select at least one member" : "Select at least one miner");
      return;
    }
    onSubmit(identifiers);
  }, [addMode, count, eligibleDevices, isAdd, isRemove, onSubmit, selectedMemberIds, target]);

  return (
    <Modal
      open
      title={mutationTitle[mode]}
      onDismiss={onDismiss}
      size={isCompactModal ? "standard" : "large"}
      className={isCompactModal ? undefined : "flex !h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"}
      bodyClassName={isCompactModal ? undefined : "flex flex-1 min-h-0 flex-col overflow-hidden"}
      buttons={[
        {
          text: mutationButton[mode],
          variant: mode === "remove" ? variants.danger : variants.primary,
          onClick: handleSubmit,
          loading: isSubmitting,
          disabled: isAdd && (!target || isLoadingEligibleDevices),
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
      <div className="mt-4 flex min-h-0 flex-1 flex-col">
        {error ? <Callout className="mb-4 shrink-0" intent="danger" prefixIcon={<Alert />} title={error} /> : null}
        {isAdd && !target ? (
          <Callout
            className="mb-4 shrink-0"
            intent="danger"
            prefixIcon={<Alert />}
            title="Adding members requires a single product and model."
          />
        ) : null}
        {isRemove ? (
          <div className="flex flex-col">
            {members.map((member, index) => {
              const memberName = cohortDeviceDisplayName(member);
              const memberSecondary = cohortMemberSecondaryText(member);

              return (
                <Row key={member.deviceIdentifier} divider={index < members.length - 1} compact>
                  <label className="flex w-full cursor-pointer items-center gap-4">
                    <Checkbox
                      checked={selectedMemberIds.has(member.deviceIdentifier)}
                      onChange={() => toggleMember(member.deviceIdentifier)}
                    />
                    <div className="min-w-0">
                      <div className="truncate text-emphasis-300 text-text-primary">{memberName}</div>
                      {memberSecondary ? (
                        <div className="truncate text-200 text-text-primary-70">{memberSecondary}</div>
                      ) : null}
                    </div>
                  </label>
                </Row>
              );
            })}
            <ModalSelectAllFooter
              label={`${selectedRemoveCount} ${selectedRemoveCount === 1 ? "member" : "members"} selected`}
              onSelectAll={() => setSelectedMemberIds(new Set(members.map((member) => member.deviceIdentifier)))}
              onSelectNone={() => setSelectedMemberIds(new Set())}
            />
          </div>
        ) : null}

        {isAdd ? (
          <div className="flex min-h-0 flex-1 flex-col gap-4">
            <Select
              id="cohort-add-mode"
              label="Add members"
              options={addMemberModeOptions}
              value={addMode}
              onChange={(value) => {
                setAddMode(value as AddMemberMode);
                setError("");
              }}
              disabled={!target}
              forceBelow
            />

            {addMode === "count" ? (
              <>
                <Input
                  id="cohort-add-count"
                  label="Count"
                  initValue={count}
                  onChange={(value) => {
                    setCount(value);
                    setError("");
                  }}
                  inputMode="numeric"
                  type="number"
                  required
                />
                <div className="rounded-lg bg-core-primary-5 px-4 py-3">
                  <div className="text-200 text-text-primary-70">Eligible miners</div>
                  <div className="mt-1 text-emphasis-300 text-text-primary">
                    {isLoadingEligibleDevices ? "Loading..." : eligibleDevices.length.toString()}
                  </div>
                </div>
              </>
            ) : null}

            {addMode === "explicit" ? (
              <div className="flex min-h-[360px] flex-1 flex-col overflow-hidden rounded-lg border border-border-5 p-3">
                <MinerSelectionList
                  ref={selectionRef}
                  fixedModels={fixedModelFilter}
                  showSelectAllFooter={false}
                  isMembersLoading={isLoadingEligibleDevices}
                  isRowVisible={(item) => eligibleDeviceIds.has(item.deviceIdentifier)}
                  noDataElement={
                    <div className="py-10 text-center text-300 text-text-primary-70">
                      {target
                        ? "No eligible miners match this cohort."
                        : "Adding members requires a single product and model."}
                    </div>
                  }
                  visibleTotal={eligibleDevices.length}
                />
              </div>
            ) : null}
          </div>
        ) : null}

        {mode === "reassign" ? (
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-border-5 p-3">
            <MinerSelectionList
              ref={selectionRef}
              showSelectAllFooter={false}
              isRowDisabled={
                target ? (item) => item.manufacturer !== target.manufacturer || item.model !== target.model : undefined
              }
            />
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

export default CohortOverviewPage;
