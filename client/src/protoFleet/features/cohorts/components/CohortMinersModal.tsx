import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import {
  CohortConfigDimension,
  CohortConfigLifecycleState,
  type CohortConfigStatus,
  type CohortDevice,
  CohortFirmwareRolloutState,
  type CohortFirmwareStatus,
  type CohortPoolDesiredConfig,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";
import usePools from "@/protoFleet/api/usePools";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import {
  cohortDeviceDisplayName,
  cohortDeviceSecondaryText,
  firmwareRolloutStateLabel,
} from "@/protoFleet/features/cohorts/utils";
import { Alert, ChevronDown, Minus, Pause, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

const pageSize = 50;
const refreshIntervalMs = Math.min(POLL_INTERVAL_MS, 3000);

type MiningPoolSummary = ReturnType<typeof usePools>["miningPools"][number];

interface CohortMinersModalProps {
  open: boolean;
  cohortId: bigint;
  cohortLabel: string;
  desiredPools?: CohortPoolDesiredConfig;
  onDismiss: () => void;
}

const configStateLabel = (state: CohortConfigLifecycleState) => {
  switch (state) {
    case CohortConfigLifecycleState.UNSUPPORTED:
      return "Unsupported";
    case CohortConfigLifecycleState.WAITING_FOR_OBSERVATION:
      return "Waiting for observation";
    case CohortConfigLifecycleState.APPLYING:
      return "Applying";
    case CohortConfigLifecycleState.VERIFYING:
      return "Verifying";
    case CohortConfigLifecycleState.CONVERGED:
      return "Complete";
    case CohortConfigLifecycleState.HELD:
      return "Held";
    case CohortConfigLifecycleState.FAILED:
      return "Failed";
    default:
      return "Unknown";
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

const configStatusTimeLabel = (status?: CohortConfigStatus) => {
  const confirmed = status?.confirmedAt ? new Date(timestampMs(status.confirmedAt)).toLocaleString() : "";
  if (confirmed) return `Confirmed ${confirmed}`;
  const observed = status?.observedAt ? new Date(timestampMs(status.observedAt)).toLocaleString() : "";
  if (observed) return `Updated ${observed}`;
  const dispatched = status?.lastDispatchedAt ? new Date(timestampMs(status.lastDispatchedAt)).toLocaleString() : "";
  return dispatched ? `Dispatched ${dispatched}` : "";
};

const FirmwareCell = ({ device }: { device: CohortDevice }) => {
  const current = device.firmwareStatus?.currentFirmwareVersion.trim() || device.display?.firmwareVersion.trim();
  const target = device.firmwareStatus?.targetFirmwareVersion.trim();
  const hasTarget = Boolean(device.firmwareStatus?.targetFirmwareFileId);

  return (
    <div className="min-w-0">
      <div className="truncate font-medium text-text-primary" title={current || "Unknown"}>
        {current || "Unknown"}
      </div>
      <div className="mt-1 truncate text-200 text-text-primary-70" title={hasTarget ? target || "Unknown" : undefined}>
        {hasTarget ? `Target: ${target || "Unknown"}` : "Not enforced"}
      </div>
    </div>
  );
};

const poolDisplayName = (poolId: bigint, miningPools: MiningPoolSummary[]) =>
  miningPools.find((pool) => pool.poolId === poolId.toString())?.name || `Pool ID ${poolId.toString()}`;

const PoolTargetCell = ({
  pools,
  miningPools,
  isLoading,
}: {
  pools?: CohortPoolDesiredConfig;
  miningPools: MiningPoolSummary[];
  isLoading: boolean;
}) => {
  if (!pools) return <span className="text-text-primary-70">Not enforced</span>;
  if (isLoading) return <span className="text-text-primary-70">Loading...</span>;

  const primary = poolDisplayName(pools.primaryPoolId, miningPools);
  const backups = [pools.backup1PoolId, pools.backup2PoolId]
    .filter((poolId): poolId is bigint => poolId !== undefined)
    .map((poolId) => poolDisplayName(poolId, miningPools));

  return (
    <div className="min-w-0">
      <div className="truncate font-medium text-text-primary" title={primary}>
        {primary}
      </div>
      {backups.length > 0 ? (
        <div className="mt-1 truncate text-200 text-text-primary-70" title={backups.join(", ")}>
          {backups.length === 1 ? "Backup" : "Backups"}: {backups.join(", ")}
        </div>
      ) : null}
    </div>
  );
};

interface ReconciliationStatusRowProps {
  icon: ReactNode;
  label: string;
  stateLabel: string;
  eventTime?: string;
  retryCount?: number;
  lastError?: string;
}

const ReconciliationStatusRow = ({
  icon,
  label,
  stateLabel,
  eventTime,
  retryCount = 0,
  lastError,
}: ReconciliationStatusRowProps) => (
  <div className="grid min-w-0 grid-cols-[20px_minmax(0,1fr)] items-start gap-x-1.5">
    <div
      className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center"
      role="img"
      aria-label={`${label}: ${stateLabel}`}
      title={stateLabel}
    >
      {icon}
    </div>
    <div className="min-w-0">
      <div className="font-medium text-text-primary">{label}</div>
      <div className="text-200 text-text-primary-70">{stateLabel}</div>
      {eventTime ? (
        <div className="truncate text-200 text-text-primary-50" title={eventTime}>
          {eventTime}
        </div>
      ) : null}
      {retryCount > 0 ? (
        <div className="text-200 text-text-primary-50">
          {retryCount} {retryCount === 1 ? "retry" : "retries"}
        </div>
      ) : null}
      {lastError ? (
        <div className="truncate text-200 text-intent-critical-fill" title={lastError}>
          {lastError}
        </div>
      ) : null}
    </div>
  </div>
);

const firmwareStatusIcon = (status: CohortFirmwareStatus | undefined, managed: boolean) => {
  if (!managed) return <Minus className="text-text-primary-50" />;
  switch (status?.state) {
    case CohortFirmwareRolloutState.COMPLETE:
      return <Success className="text-intent-success-fill" />;
    case CohortFirmwareRolloutState.QUEUED:
    case CohortFirmwareRolloutState.UPDATING:
    case CohortFirmwareRolloutState.VERIFYING:
      return <ProgressCircular className="text-core-primary-fill" indeterminate size={16} />;
    case CohortFirmwareRolloutState.NEEDS_ATTENTION:
      return <Alert className="text-intent-critical-fill" width="w-[18px]" />;
    default:
      return <Minus className="text-text-primary-50" />;
  }
};

const configStatusIcon = (state: CohortConfigLifecycleState, managed: boolean) => {
  if (!managed) return <Minus className="text-text-primary-50" />;
  switch (state) {
    case CohortConfigLifecycleState.CONVERGED:
      return <Success className="text-intent-success-fill" />;
    case CohortConfigLifecycleState.WAITING_FOR_OBSERVATION:
    case CohortConfigLifecycleState.APPLYING:
    case CohortConfigLifecycleState.VERIFYING:
      return <ProgressCircular className="text-core-primary-fill" indeterminate size={16} />;
    case CohortConfigLifecycleState.HELD:
      return <Pause className="text-intent-warning-fill" width="w-[18px]" />;
    case CohortConfigLifecycleState.FAILED:
      return <Alert className="text-intent-critical-fill" width="w-[18px]" />;
    default:
      return <Minus className="text-text-primary-50" />;
  }
};

const ReconciliationCell = ({
  firmwareStatus,
  poolsManaged,
  configStatuses,
}: {
  firmwareStatus?: CohortFirmwareStatus;
  poolsManaged: boolean;
  configStatuses: CohortConfigStatus[];
}) => {
  const firmwareManaged = Boolean(firmwareStatus?.targetFirmwareFileId);
  const firmwareStateLabel =
    firmwareManaged && firmwareStatus ? firmwareRolloutStateLabel(firmwareStatus.state) : "Not enforced";
  const firmwareError = firmwareStatus?.lastError.trim();
  const poolStatus = configStatuses.find((status) => status.dimension === CohortConfigDimension.POOLS);
  const poolState = poolStatus?.state ?? CohortConfigLifecycleState.WAITING_FOR_OBSERVATION;
  const poolStateLabel = poolsManaged ? configStateLabel(poolState) : "Not enforced";
  const poolError = poolStatus?.lastError.trim();

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <ReconciliationStatusRow
        icon={firmwareStatusIcon(firmwareStatus, firmwareManaged)}
        label="Firmware"
        stateLabel={firmwareStateLabel}
        eventTime={firmwareManaged ? firmwareStatusTimeLabel(firmwareStatus) : undefined}
        retryCount={firmwareManaged ? firmwareStatus?.retryCount : 0}
        lastError={firmwareManaged ? firmwareError : undefined}
      />
      <ReconciliationStatusRow
        icon={configStatusIcon(poolState, poolsManaged)}
        label="Pools"
        stateLabel={poolStateLabel}
        eventTime={poolsManaged ? configStatusTimeLabel(poolStatus) : undefined}
        retryCount={poolsManaged ? poolStatus?.retryCount : 0}
        lastError={poolsManaged ? poolError : undefined}
      />
    </div>
  );
};

const isDeviceConverging = (device: CohortDevice, poolsManaged: boolean) => {
  const firmwareState = device.firmwareStatus?.state;
  const firmwareConverging =
    Boolean(device.firmwareStatus?.targetFirmwareFileId) &&
    (firmwareState === CohortFirmwareRolloutState.QUEUED ||
      firmwareState === CohortFirmwareRolloutState.UPDATING ||
      firmwareState === CohortFirmwareRolloutState.VERIFYING);
  const poolsConverging =
    poolsManaged &&
    device.configStatuses.some(
      (status) =>
        status.dimension === CohortConfigDimension.POOLS &&
        (status.state === CohortConfigLifecycleState.WAITING_FOR_OBSERVATION ||
          status.state === CohortConfigLifecycleState.APPLYING ||
          status.state === CohortConfigLifecycleState.VERIFYING),
    );
  return firmwareConverging || poolsConverging;
};

const CohortMinersModal = ({ open, cohortId, cohortLabel, desiredPools, onDismiss }: CohortMinersModalProps) => {
  const { listDevices } = useCohortApi();
  const { miningPools, isLoading: poolsLoading } = usePools(Boolean(desiredPools));
  const [devices, setDevices] = useState<CohortDevice[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const [search, setSearch] = useState("");
  const [pageToken, setPageToken] = useState("");
  const [nextPageToken, setNextPageToken] = useState("");
  const [pageHistory, setPageHistory] = useState<string[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const requestIdRef = useRef(0);

  const loadDevices = useCallback(
    async (showLoading: boolean) => {
      if (!open) return;
      const requestId = requestIdRef.current + 1;
      requestIdRef.current = requestId;
      if (showLoading) setLoading(true);
      setError(false);
      try {
        const result = await listDevices({
          pageSize,
          pageToken,
          filter: { cohortIds: [cohortId], search },
        });
        if (requestIdRef.current !== requestId) return;
        setDevices(result.devices);
        setNextPageToken(result.nextPageToken);
        setTotalCount(result.totalCount);
      } catch {
        if (requestIdRef.current === requestId) setError(true);
      } finally {
        if (showLoading && requestIdRef.current === requestId) setLoading(false);
      }
    },
    [cohortId, listDevices, open, pageToken, search],
  );

  useEffect(() => {
    if (!open) return;
    queueMicrotask(() => void loadDevices(true));
  }, [loadDevices, open]);

  const hasConvergingRows = useMemo(
    () => devices.some((device) => isDeviceConverging(device, Boolean(desiredPools))),
    [desiredPools, devices],
  );
  useEffect(() => {
    if (!open || !hasConvergingRows) return undefined;
    const intervalId = window.setInterval(() => void loadDevices(false), refreshIntervalMs);
    return () => window.clearInterval(intervalId);
  }, [hasConvergingRows, loadDevices, open]);

  const handleSearchChange = useCallback((value: string) => {
    setSearch(value);
    setPageToken("");
    setPageHistory([]);
  }, []);

  const goToNextPage = useCallback(() => {
    if (!nextPageToken) return;
    setPageHistory((history) => [...history, pageToken]);
    setPageToken(nextPageToken);
  }, [nextPageToken, pageToken]);

  const goToPreviousPage = useCallback(() => {
    setPageHistory((history) => {
      if (history.length === 0) return history;
      setPageToken(history[history.length - 1] ?? "");
      return history.slice(0, -1);
    });
  }, []);

  const firstItemIndex = pageHistory.length * pageSize + 1;
  const lastItemIndex = pageHistory.length * pageSize + devices.length;

  return (
    <Modal
      open={open}
      title={`Miners in ${cohortLabel}`}
      description="Inspect each miner's desired state and reconciliation progress. Membership changes remain in the cohort actions."
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      onDismiss={onDismiss}
      divider={false}
      testId="cohort-miners-modal"
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <div className="shrink-0">
          <Input
            id="cohort-miners-search"
            label="Search miners"
            initValue={search}
            onChange={handleSearchChange}
            testId="cohort-miners-search"
          />
        </div>

        {error ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-4" data-testid="cohort-miners-error">
            <Callout intent="danger" prefixIcon={<Alert />} title="Couldn't load cohort miners" />
            <Button text="Retry" variant={variants.secondary} onClick={() => void loadDevices(true)} />
          </div>
        ) : loading ? (
          <div className="flex flex-1 items-center justify-center" data-testid="cohort-miners-loading">
            <ProgressCircular indeterminate />
          </div>
        ) : (
          <>
            <div className="min-h-0 flex-1 overflow-auto rounded-lg border border-border-5">
              <table className="w-full min-w-[900px] table-fixed text-left text-300">
                <thead className="bg-surface-raised sticky top-0 z-1 text-text-primary-70">
                  <tr>
                    <th className="w-[28%] px-4 py-3 font-medium">Miner</th>
                    <th className="w-[18%] px-3 py-3 font-medium">Firmware</th>
                    <th className="w-[22%] px-3 py-3 font-medium">Desired pools</th>
                    <th className="w-[32%] px-3 py-3 font-medium">Reconciliation status</th>
                  </tr>
                </thead>
                <tbody>
                  {devices.map((device) => {
                    const name = cohortDeviceDisplayName(device);
                    const secondary = cohortDeviceSecondaryText(device.display, name);
                    return (
                      <tr key={device.deviceIdentifier} className="border-t border-border-5 align-top">
                        <td className="px-4 py-3">
                          <div className="truncate font-medium text-text-primary" title={device.deviceIdentifier}>
                            {name}
                          </div>
                          {secondary ? (
                            <div className="mt-1 truncate text-200 text-text-primary-70" title={secondary}>
                              {secondary}
                            </div>
                          ) : null}
                        </td>
                        <td className="px-3 py-3">
                          <FirmwareCell device={device} />
                        </td>
                        <td className="px-3 py-3">
                          <PoolTargetCell pools={desiredPools} miningPools={miningPools} isLoading={poolsLoading} />
                        </td>
                        <td className="px-3 py-3">
                          <ReconciliationCell
                            firmwareStatus={device.firmwareStatus}
                            poolsManaged={Boolean(desiredPools)}
                            configStatuses={device.configStatuses}
                          />
                        </td>
                      </tr>
                    );
                  })}
                  {devices.length === 0 ? (
                    <tr>
                      <td className="px-4 py-12 text-center text-text-primary-70" colSpan={4}>
                        {search.trim() ? "No miners match this search." : "No miners in this cohort."}
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>

            {devices.length > 0 || pageHistory.length > 0 || nextPageToken ? (
              <div className="flex shrink-0 flex-wrap items-center justify-between gap-3">
                <span className="text-300 text-text-primary">
                  Showing {firstItemIndex}-{lastItemIndex} of {totalCount} {totalCount === 1 ? "miner" : "miners"}
                </span>
                <div className="flex gap-3">
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    ariaLabel="Previous miners page"
                    prefixIcon={<ChevronDown className="rotate-90" />}
                    onClick={goToPreviousPage}
                    disabled={pageHistory.length === 0}
                  />
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    ariaLabel="Next miners page"
                    prefixIcon={<ChevronDown className="rotate-270" />}
                    onClick={goToNextPage}
                    disabled={!nextPageToken}
                  />
                </div>
              </div>
            ) : null}
          </>
        )}
      </div>
    </Modal>
  );
};

export default CohortMinersModal;
