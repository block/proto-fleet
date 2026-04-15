import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { type DragEndEvent } from "@dnd-kit/core";

import {
  bulkRenameModes,
  type BulkRenamePreferences,
  type BulkRenamePreviewMiner,
  type BulkRenamePropertyId,
  type BulkRenamePropertyOptions,
  reorderBulkRenameProperties,
  shouldWarnAboutBulkRenameDuplicates,
  updateBulkRenameProperty,
} from "./bulkRenameDefinitions";
import BulkRenameDialogs from "./BulkRenameDialogs";
import BulkRenameOptionModals from "./BulkRenameOptionModals";
import {
  buildBulkRenameConfig,
  buildBulkRenamePropertyPreview,
  evaluateBulkRenamePreviewName,
  findBulkRenamePropertyPreviewMinerIndex,
  getMinerPreviewName,
  mapSnapshotsToBulkRenamePreviewMiners,
  mapSnapshotToBulkRenamePreviewMiner,
  shouldShowBulkRenameNoChangesWarning,
  takePreviewMiners,
} from "./bulkRenamePreview";
import BulkRenamePreviewPanel, { type PreviewRow } from "./BulkRenamePreviewPanel";
import BulkRenamePropertyForm from "./BulkRenamePropertyForm";
import { settingsActions } from "./constants";
import {
  type CustomPropertyOptionsValues,
  type FixedValueOptionsValues,
  type QualifierOptionsValues,
} from "./RenameOptionsModals/types";
import { waitForWorkerNameBatchResult, type WorkerNameBatchResult } from "./waitForWorkerNameBatchResult";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  type SortConfig,
  SortConfigSchema,
  SortDirection,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type DeviceSelector,
  DeviceSelectorSchema,
  type MinerListFilter,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import useUpdateWorkerNames from "@/protoFleet/api/useUpdateWorkerNames";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { useBatchOperations } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import {
  applyFleetSelectablePairingStatuses,
  isFleetSelectablePairingStatus,
} from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { useAuthErrors, useBulkWorkerNamePreferences, useSetBulkWorkerNamePreferences } from "@/protoFleet/store";
import { Info } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import { type SelectionMode } from "@/shared/components/List";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

interface BulkWorkerNameModalProps {
  open: boolean;
  selectedMinerIds: string[];
  selectionMode: SelectionMode;
  originalSelectionMode?: SelectionMode;
  totalCount?: number;
  currentFilter?: MinerListFilter;
  currentSort?: SortConfig;
  miners: Record<string, MinerStateSnapshot>;
  minerIds: string[];
  onRefetchMiners?: () => void;
  onWorkerNameUpdated?: (deviceIdentifier: string, workerName: string) => void;
  getWorkerNameCredentials?: () => { username: string; password: string } | undefined;
  onDismiss: () => void;
}

const duplicateNamesDialogBody =
  "Some miners may have duplicate worker names. Proceeding may impact accuracy in pool dashboards. Do you want to continue anyway?";
const noChangesDialogBody =
  "You can continue to retain your existing worker names, or keep editing. Do you want to continue anyway?";
const overwriteDialogBody =
  "This will replace existing worker names for the selected miners in Fleet. The apply action will also update current pool settings to use the new worker names.";
const emptyOptionsPreview = {
  previewName: "",
  highlightedText: undefined,
  highlightStartIndex: undefined,
} as const;
const previewSortCollator = new Intl.Collator(undefined, {
  numeric: true,
  sensitivity: "base",
});

function getSelectionCount(selectionMode: SelectionMode, selectedMinerIds: string[], totalCount?: number): number {
  if (selectionMode === "all") {
    return totalCount ?? selectedMinerIds.length;
  }

  return selectedMinerIds.length;
}

function computePreviewNames(preferences: BulkRenamePreferences, previewMiners: BulkRenamePreviewMiner[]): string[] {
  const config = buildBulkRenameConfig(preferences);
  return previewMiners.map((miner) => evaluateBulkRenamePreviewName(config, miner, miner.counterIndex));
}

function buildVisibleWorkerNamesByDeviceIdentifier(
  preferences: BulkRenamePreferences,
  previewMiners: BulkRenamePreviewMiner[],
): Record<string, string> {
  const config = buildBulkRenameConfig(preferences);

  return Object.fromEntries(
    previewMiners
      .map(
        (miner) => [miner.deviceIdentifier, evaluateBulkRenamePreviewName(config, miner, miner.counterIndex)] as const,
      )
      .filter(([, workerName]) => workerName.trim() !== ""),
  );
}

function getPreviewRows(previewMiners: BulkRenamePreviewMiner[], previewNames: string[]): PreviewRow[] {
  return previewMiners.map((miner, index) => ({
    currentName: miner.currentName,
    newName: previewNames[index] ?? "",
  }));
}

function buildOptionsPreviewPreferences(
  preferences: BulkRenamePreferences,
  propertyId: BulkRenamePropertyId,
  options: BulkRenamePropertyOptions | null,
): BulkRenamePreferences {
  return updateBulkRenameProperty(preferences, propertyId, (property) => ({
    ...property,
    enabled: true,
    options: options ?? property.options,
  }));
}

function formatMinerCount(count: number): string {
  return `${count} miner${count === 1 ? "" : "s"}`;
}

function getBulkWorkerNameLoadingMessage(selectionCount: number): string {
  return selectionCount === 1 ? "Updating worker name" : "Updating worker names";
}

function getBulkWorkerNameSuccessMessage(updatedCount: number, unchangedCount: number): string {
  if (unchangedCount === 0) {
    return `Updated ${formatMinerCount(updatedCount)}`;
  }

  if (updatedCount === 0) {
    return `${formatMinerCount(unchangedCount)} unchanged`;
  }

  return `Updated ${formatMinerCount(updatedCount)}; ${formatMinerCount(unchangedCount)} unchanged`;
}

function getBulkWorkerNameFailureMessage(failedCount: number): string {
  return `Failed to update worker names for ${formatMinerCount(failedCount)}`;
}

function getBulkWorkerNameRequestFailureMessage(selectionCount: number): string {
  return selectionCount === 1 ? "Failed to update worker name" : "Failed to update worker names";
}

function getVisibleSuccessfulWorkerNameDeviceIds(
  submittedWorkerNamesByDeviceIdentifier: Record<string, string>,
  failedCount: number,
): string[] {
  if (failedCount > 0) {
    return [];
  }

  return Object.keys(submittedWorkerNamesByDeviceIdentifier);
}

function getLatestMeasurementValue(
  measurements:
    | MinerStateSnapshot["powerUsage"]
    | MinerStateSnapshot["temperature"]
    | MinerStateSnapshot["hashrate"]
    | MinerStateSnapshot["efficiency"],
): number | undefined {
  return getLatestMeasurementWithData(measurements)?.value;
}

function compareSnapshotMetric(
  leftValue: number | undefined,
  rightValue: number | undefined,
  direction: SortDirection,
): number {
  if (leftValue === undefined && rightValue === undefined) {
    return 0;
  }

  if (leftValue === undefined) {
    return 1;
  }

  if (rightValue === undefined) {
    return -1;
  }

  const difference = leftValue - rightValue;
  return direction === SortDirection.DESC ? -difference : difference;
}

function compareSnapshotText(leftValue: string, rightValue: string, direction: SortDirection): number {
  const comparison = previewSortCollator.compare(leftValue, rightValue);
  return direction === SortDirection.DESC ? -comparison : comparison;
}

function normalizeNullableSnapshotText(value: string): string | null {
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

function compareNullableSnapshotText(leftValue: string, rightValue: string, direction: SortDirection): number {
  const leftText = normalizeNullableSnapshotText(leftValue);
  const rightText = normalizeNullableSnapshotText(rightValue);

  if (leftText === null && rightText === null) {
    return 0;
  }

  if (leftText === null) {
    return 1;
  }

  if (rightText === null) {
    return -1;
  }

  return compareSnapshotText(leftText, rightText, direction);
}

function compareMinerSnapshots(left: MinerStateSnapshot, right: MinerStateSnapshot, previewSort: SortConfig): number {
  const direction = previewSort.direction;

  switch (previewSort.field) {
    case SortField.WORKER_NAME:
      return compareNullableSnapshotText(left.workerName, right.workerName, direction);
    case SortField.IP_ADDRESS:
      return compareSnapshotText(left.ipAddress, right.ipAddress, direction);
    case SortField.MAC_ADDRESS:
      return compareSnapshotText(left.macAddress, right.macAddress, direction);
    case SortField.MODEL:
      return compareSnapshotText(left.model, right.model, direction);
    case SortField.HASHRATE:
      return compareSnapshotMetric(
        getLatestMeasurementValue(left.hashrate),
        getLatestMeasurementValue(right.hashrate),
        direction,
      );
    case SortField.TEMPERATURE:
      return compareSnapshotMetric(
        getLatestMeasurementValue(left.temperature),
        getLatestMeasurementValue(right.temperature),
        direction,
      );
    case SortField.POWER:
      return compareSnapshotMetric(
        getLatestMeasurementValue(left.powerUsage),
        getLatestMeasurementValue(right.powerUsage),
        direction,
      );
    case SortField.EFFICIENCY:
      return compareSnapshotMetric(
        getLatestMeasurementValue(left.efficiency),
        getLatestMeasurementValue(right.efficiency),
        direction,
      );
    case SortField.FIRMWARE:
      return compareSnapshotText(left.firmwareVersion, right.firmwareVersion, direction);
    case SortField.UNSPECIFIED:
    case SortField.NAME:
    default:
      return compareSnapshotText(getMinerPreviewName(left), getMinerPreviewName(right), direction);
  }
}

function sortMinerSnapshotsByPreviewSort(
  snapshots: MinerStateSnapshot[],
  previewSort: SortConfig,
): MinerStateSnapshot[] {
  return snapshots
    .map((snapshot, index) => ({ snapshot, index }))
    .sort((left, right) => {
      const comparison = compareMinerSnapshots(left.snapshot, right.snapshot, previewSort);
      return comparison !== 0 ? comparison : left.index - right.index;
    })
    .map(({ snapshot }) => snapshot);
}

type WorkerNameUpdateCompletion = {
  updatedCount: number;
  unchangedCount: number;
  failedCount: number;
  successfulDeviceIds?: string[];
  submittedWorkerNamesByDeviceIdentifier?: Record<string, string>;
};

function createBulkWorkerNameDeviceSelector(
  selectionMode: SelectionMode,
  currentFilter: MinerListFilter | undefined,
  selectedMinerIds: string[],
): DeviceSelector {
  const selectionType =
    selectionMode === "all"
      ? {
          case: "allDevices" as const,
          value: applyFleetSelectablePairingStatuses(currentFilter),
        }
      : {
          case: "includeDevices" as const,
          value: create(DeviceIdentifierListSchema, {
            deviceIdentifiers: selectedMinerIds,
          }),
        };

  return create(DeviceSelectorSchema, { selectionType });
}

const BulkWorkerNameModal = ({
  open,
  selectedMinerIds,
  selectionMode,
  originalSelectionMode,
  totalCount,
  currentFilter,
  currentSort,
  miners: minersById,
  minerIds,
  onRefetchMiners,
  onWorkerNameUpdated,
  getWorkerNameCredentials,
  onDismiss,
}: BulkWorkerNameModalProps) => {
  const { startBatchOperation, completeBatchOperation } = useBatchOperations();
  const preferences = useBulkWorkerNamePreferences();
  const setBulkWorkerNamePreferences = useSetBulkWorkerNamePreferences();
  const { handleAuthErrors } = useAuthErrors();
  const { streamCommandBatchUpdates } = useMinerCommand();
  const { updateWorkerNames } = useUpdateWorkerNames();
  const { isPhone, isTablet } = useWindowDimensions();

  const [previewMiners, setPreviewMiners] = useState<BulkRenamePreviewMiner[]>([]);
  const [previewNames, setPreviewNames] = useState<string[]>([]);
  const [showPreviewEllipsis, setShowPreviewEllipsis] = useState(false);
  const [isLoadingPreview, setIsLoadingPreview] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [activeOptionsPropertyId, setActiveOptionsPropertyId] = useState<BulkRenamePropertyId | null>(null);
  const [activeOptionsDraft, setActiveOptionsDraft] = useState<BulkRenamePropertyOptions | null>(null);
  const [showDuplicateNamesWarning, setShowDuplicateNamesWarning] = useState(false);
  const [showNoChangesWarning, setShowNoChangesWarning] = useState(false);
  const [showOverwriteWarning, setShowOverwriteWarning] = useState(false);
  const preferencesRef = useRef(preferences);
  const previewMinersRef = useRef(previewMiners);

  const selectionCount = useMemo(
    () => getSelectionCount(selectionMode, selectedMinerIds, totalCount),
    [selectionMode, selectedMinerIds, totalCount],
  );
  const overwriteFallbackSelectionMode = originalSelectionMode ?? selectionMode;
  const previewSampleSize = useMemo(() => (isPhone || isTablet ? 1 : 6), [isPhone, isTablet]);
  const previewSort = useMemo(
    () =>
      currentSort ??
      create(SortConfigSchema, {
        field: SortField.NAME,
        direction: SortDirection.ASC,
      }),
    [currentSort],
  );
  const selectedMinerIdSet = useMemo(() => new Set(selectedMinerIds), [selectedMinerIds]);
  const localPreviewSnapshots = useMemo(() => {
    const snapshots =
      selectionMode === "subset"
        ? minerIds
            .filter((deviceIdentifier) => selectedMinerIdSet.has(deviceIdentifier))
            .map((deviceIdentifier) => minersById[deviceIdentifier])
            .filter((miner): miner is NonNullable<typeof miner> => miner !== undefined)
        : minerIds
            .map((deviceIdentifier) => minersById[deviceIdentifier])
            .filter(
              (miner): miner is NonNullable<typeof miner> =>
                miner !== undefined && isFleetSelectablePairingStatus(miner.pairingStatus),
            );

    return sortMinerSnapshotsByPreviewSort(snapshots, previewSort);
  }, [minerIds, minersById, previewSort, selectedMinerIdSet, selectionMode]);
  const localPreviewMiners = useMemo(
    () => mapSnapshotsToBulkRenamePreviewMiners(localPreviewSnapshots, bulkRenameModes.worker),
    [localPreviewSnapshots],
  );

  const localValidationMiners = useMemo(() => {
    if (selectionMode === "subset") {
      return localPreviewMiners.length === selectedMinerIds.length ? localPreviewMiners : null;
    }

    return localPreviewMiners.length === selectionCount ? localPreviewMiners : null;
  }, [localPreviewMiners, selectedMinerIds.length, selectionCount, selectionMode]);
  const canOptimisticallyUpdateVisibleWorkerNames = useMemo(
    () => selectionMode === "subset" && localValidationMiners !== null,
    [localValidationMiners, selectionMode],
  );

  const applyVisibleWorkerNameUpdates = useCallback(
    (successfulDeviceIds: string[], submittedWorkerNamesByDeviceIdentifier: Record<string, string>) => {
      if (!canOptimisticallyUpdateVisibleWorkerNames) {
        return;
      }

      successfulDeviceIds.forEach((deviceIdentifier) => {
        const workerName = submittedWorkerNamesByDeviceIdentifier[deviceIdentifier];
        if (workerName !== undefined) {
          onWorkerNameUpdated?.(deviceIdentifier, workerName);
        }
      });
    },
    [canOptimisticallyUpdateVisibleWorkerNames, onWorkerNameUpdated],
  );

  const finishWorkerNameUpdate = useCallback(
    (toastId: number, completion: WorkerNameUpdateCompletion) => {
      const {
        updatedCount,
        unchangedCount,
        failedCount,
        successfulDeviceIds = [],
        submittedWorkerNamesByDeviceIdentifier = {},
      } = completion;

      applyVisibleWorkerNameUpdates(successfulDeviceIds, submittedWorkerNamesByDeviceIdentifier);
      onRefetchMiners?.();

      if (updatedCount > 0 || unchangedCount > 0) {
        updateToast(toastId, {
          message: getBulkWorkerNameSuccessMessage(updatedCount, unchangedCount),
          status: TOAST_STATUSES.success,
        });
      } else if (failedCount > 0) {
        updateToast(toastId, {
          message: getBulkWorkerNameFailureMessage(failedCount),
          status: TOAST_STATUSES.error,
        });
      } else {
        removeToast(toastId);
      }

      if (failedCount > 0 && (updatedCount > 0 || unchangedCount > 0)) {
        pushToast({
          message: getBulkWorkerNameFailureMessage(failedCount),
          status: TOAST_STATUSES.error,
          longRunning: true,
        });
      }

      onDismiss();
    },
    [applyVisibleWorkerNameUpdates, onDismiss, onRefetchMiners],
  );

  const handleWorkerNameBatchRequestFailure = useCallback(
    (toastId: number) => {
      updateToast(toastId, {
        message: getBulkWorkerNameRequestFailureMessage(selectionCount),
        status: TOAST_STATUSES.error,
      });
      onRefetchMiners?.();
      onDismiss();
    },
    [onDismiss, onRefetchMiners, selectionCount],
  );

  const loadPreviewMiners = useCallback(async (): Promise<{
    miners: BulkRenamePreviewMiner[];
    showEllipsis: boolean;
  }> => {
    if (selectionMode === "subset") {
      return takePreviewMiners(localPreviewMiners, selectionCount, previewSampleSize);
    }

    if (previewSampleSize === 1) {
      const filter = applyFleetSelectablePairingStatuses(currentFilter);
      const response = await fleetManagementClient.listMinerStateSnapshots({
        pageSize: 1,
        filter,
        sort: [previewSort],
      });

      return {
        miners: mapSnapshotsToBulkRenamePreviewMiners(response.miners, bulkRenameModes.worker),
        showEllipsis: false,
      };
    }

    if (localValidationMiners !== null) {
      return takePreviewMiners(localValidationMiners, selectionCount, previewSampleSize);
    }

    const filter = applyFleetSelectablePairingStatuses(currentFilter);
    const sort = [previewSort];
    const reverseSort = [
      create(SortConfigSchema, {
        field: previewSort.field,
        direction: previewSort.direction === SortDirection.DESC ? SortDirection.ASC : SortDirection.DESC,
      }),
    ];

    if (selectionCount <= previewSampleSize) {
      const response = await fleetManagementClient.listMinerStateSnapshots({
        pageSize: selectionCount,
        filter,
        sort,
      });

      return {
        miners: mapSnapshotsToBulkRenamePreviewMiners(response.miners, bulkRenameModes.worker),
        showEllipsis: false,
      };
    }

    const headPreviewCount = Math.floor(previewSampleSize / 2);
    const tailPreviewCount = previewSampleSize - headPreviewCount;

    const [firstResponse, lastResponse] = await Promise.all([
      fleetManagementClient.listMinerStateSnapshots({
        pageSize: headPreviewCount,
        filter,
        sort,
      }),
      fleetManagementClient.listMinerStateSnapshots({
        pageSize: tailPreviewCount,
        filter,
        sort: reverseSort,
      }),
    ]);

    return {
      miners: [
        ...firstResponse.miners.map((miner, index) =>
          mapSnapshotToBulkRenamePreviewMiner(miner, index, bulkRenameModes.worker),
        ),
        ...lastResponse.miners
          .map((miner, index) =>
            mapSnapshotToBulkRenamePreviewMiner(miner, selectionCount - index - 1, bulkRenameModes.worker),
          )
          .reverse(),
      ],
      showEllipsis: true,
    };
  }, [
    currentFilter,
    localValidationMiners,
    localPreviewMiners,
    previewSampleSize,
    previewSort,
    selectionCount,
    selectionMode,
  ]);

  useEffect(() => {
    if (!open) {
      setActiveOptionsPropertyId(null);
      setActiveOptionsDraft(null);
      setShowDuplicateNamesWarning(false);
      setShowNoChangesWarning(false);
      setShowOverwriteWarning(false);
      setPreviewMiners([]);
      setPreviewNames([]);
      setShowPreviewEllipsis(false);
      setIsLoadingPreview(false);
      return;
    }

    let cancelled = false;

    const load = async () => {
      setIsLoadingPreview(true);

      try {
        const previewResult = await loadPreviewMiners();

        if (cancelled) {
          return;
        }

        setPreviewMiners(previewResult.miners);
        setShowPreviewEllipsis(previewResult.showEllipsis);
      } catch (error) {
        handleAuthErrors({
          error,
          onError: () => {
            if (cancelled) {
              return;
            }
            setPreviewMiners([]);
            setShowPreviewEllipsis(false);
          },
        });
      } finally {
        if (!cancelled) {
          setIsLoadingPreview(false);
        }
      }
    };

    void load();

    return () => {
      cancelled = true;
    };
  }, [handleAuthErrors, loadPreviewMiners, open]);

  useEffect(() => {
    preferencesRef.current = preferences;
  }, [preferences]);

  useEffect(() => {
    previewMinersRef.current = previewMiners;
  }, [previewMiners]);

  useEffect(() => {
    if (!open) {
      return;
    }

    setPreviewNames(computePreviewNames(preferencesRef.current, previewMiners));
  }, [open, previewMiners]);

  useEffect(() => {
    if (!open) {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setPreviewNames(computePreviewNames(preferences, previewMinersRef.current));
    }, 500);

    return () => {
      window.clearTimeout(timeoutId);
    };
  }, [preferences, open]);

  const handleToggleEnabled = useCallback(
    (propertyId: BulkRenamePropertyId, enabled: boolean) => {
      setBulkWorkerNamePreferences(
        updateBulkRenameProperty(preferences, propertyId, (property) => ({
          ...property,
          enabled,
        })),
      );
    },
    [preferences, setBulkWorkerNamePreferences],
  );

  const handleUpdateOptions = useCallback(
    (
      propertyId: BulkRenamePropertyId,
      options: CustomPropertyOptionsValues | FixedValueOptionsValues | QualifierOptionsValues,
    ) => {
      setBulkWorkerNamePreferences(
        updateBulkRenameProperty(preferences, propertyId, (property) => ({
          ...property,
          options,
        })),
      );
      setActiveOptionsDraft(null);
      setActiveOptionsPropertyId(null);
    },
    [preferences, setBulkWorkerNamePreferences],
  );

  const handleDismissOptions = useCallback(() => {
    setActiveOptionsDraft(null);
    setActiveOptionsPropertyId(null);
  }, []);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;

      if (!over || active.id === over.id) {
        return;
      }

      setBulkWorkerNamePreferences(
        reorderBulkRenameProperties(preferences, active.id as BulkRenamePropertyId, over.id as BulkRenamePropertyId),
      );
    },
    [preferences, setBulkWorkerNamePreferences],
  );

  const proceedWithSubmit = useCallback(
    async (username: string, password: string) => {
      const config = buildBulkRenameConfig(preferences);
      if (config.properties.length === 0) {
        pushToast({
          message: "Enable at least one worker name property",
          status: TOAST_STATUSES.error,
        });
        return;
      }

      const submittedWorkerNamesByDeviceIdentifier = canOptimisticallyUpdateVisibleWorkerNames
        ? buildVisibleWorkerNamesByDeviceIdentifier(preferences, localValidationMiners ?? [])
        : {};
      const deviceSelector = createBulkWorkerNameDeviceSelector(selectionMode, currentFilter, selectedMinerIds);

      const toastId = pushToast({
        message: getBulkWorkerNameLoadingMessage(selectionCount),
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });

      setIsSubmitting(true);

      try {
        const response = await updateWorkerNames(deviceSelector, config, username, password, previewSort);
        const unchangedCount = Number(response.unchangedCount || 0);
        const failedCount = Number(response.failedCount || 0);

        if (response.batchIdentifier) {
          startBatchOperation({
            batchIdentifier: response.batchIdentifier,
            action: settingsActions.updateWorkerNames,
            deviceIdentifiers: selectedMinerIds,
          });

          let batchResult: WorkerNameBatchResult;
          try {
            batchResult = await waitForWorkerNameBatchResult(streamCommandBatchUpdates, response.batchIdentifier);
          } finally {
            completeBatchOperation(response.batchIdentifier);
          }

          if (batchResult.streamFailed) {
            handleWorkerNameBatchRequestFailure(toastId);
            return;
          }

          finishWorkerNameUpdate(toastId, {
            updatedCount: batchResult.successCount,
            unchangedCount,
            failedCount: failedCount + batchResult.failedCount,
            successfulDeviceIds: batchResult.successDeviceIds,
            submittedWorkerNamesByDeviceIdentifier,
          });
          return;
        }

        finishWorkerNameUpdate(toastId, {
          updatedCount: Number(response.updatedCount || 0),
          unchangedCount,
          failedCount,
          successfulDeviceIds: getVisibleSuccessfulWorkerNameDeviceIds(
            submittedWorkerNamesByDeviceIdentifier,
            failedCount,
          ),
          submittedWorkerNamesByDeviceIdentifier,
        });
      } catch {
        updateToast(toastId, {
          message: getBulkWorkerNameRequestFailureMessage(selectionCount),
          status: TOAST_STATUSES.error,
        });
      } finally {
        setIsSubmitting(false);
      }
    },
    [
      completeBatchOperation,
      finishWorkerNameUpdate,
      handleWorkerNameBatchRequestFailure,
      currentFilter,
      canOptimisticallyUpdateVisibleWorkerNames,
      localValidationMiners,
      preferences,
      previewSort,
      selectedMinerIds,
      selectionCount,
      selectionMode,
      startBatchOperation,
      streamCommandBatchUpdates,
      updateWorkerNames,
    ],
  );

  const submitWithAuthenticatedCredentials = useCallback(() => {
    const credentials = getWorkerNameCredentials?.();

    if (!credentials) {
      return;
    }

    void proceedWithSubmit(credentials.username, credentials.password);
  }, [getWorkerNameCredentials, proceedWithSubmit]);

  const noChangeValidationMiners = useMemo(() => {
    if (previewMiners.length === selectionCount) {
      return previewMiners;
    }

    if (localValidationMiners !== null) {
      return localValidationMiners;
    }

    return null;
  }, [localValidationMiners, previewMiners, selectionCount]);

  const shouldShowNoChangesWarning = useMemo(
    () => shouldShowBulkRenameNoChangesWarning(preferences, noChangeValidationMiners),
    [preferences, noChangeValidationMiners],
  );

  const overwriteValidationMiners = useMemo(() => {
    if (previewMiners.length === selectionCount) {
      return previewMiners;
    }

    return localValidationMiners;
  }, [localValidationMiners, previewMiners, selectionCount]);

  const shouldShowOverwriteConfirmation = useMemo(() => {
    if (overwriteValidationMiners !== null) {
      return overwriteValidationMiners.some((miner) => miner.storedName.trim() !== "");
    }

    return overwriteFallbackSelectionMode === "all";
  }, [overwriteFallbackSelectionMode, overwriteValidationMiners]);

  const handleSubmit = useCallback(() => {
    if (shouldShowNoChangesWarning) {
      setShowNoChangesWarning(true);
      return;
    }

    if (shouldWarnAboutBulkRenameDuplicates(selectionCount, preferences, noChangeValidationMiners)) {
      setShowDuplicateNamesWarning(true);
      return;
    }

    if (shouldShowOverwriteConfirmation) {
      setShowOverwriteWarning(true);
      return;
    }

    submitWithAuthenticatedCredentials();
  }, [
    noChangeValidationMiners,
    preferences,
    selectionCount,
    shouldShowNoChangesWarning,
    shouldShowOverwriteConfirmation,
    submitWithAuthenticatedCredentials,
  ]);

  const handleDuplicateNamesContinue = useCallback(() => {
    setShowDuplicateNamesWarning(false);

    if (shouldShowOverwriteConfirmation) {
      setShowOverwriteWarning(true);
      return;
    }

    submitWithAuthenticatedCredentials();
  }, [shouldShowOverwriteConfirmation, submitWithAuthenticatedCredentials]);

  const activeOptionsProperty = useMemo(
    () => preferences.properties.find((property) => property.id === activeOptionsPropertyId) ?? null,
    [activeOptionsPropertyId, preferences.properties],
  );

  const activeOptionsPreview = useMemo(() => {
    if (activeOptionsProperty === null || previewMiners.length === 0) {
      return emptyOptionsPreview;
    }

    const previewPreferences = buildOptionsPreviewPreferences(
      preferences,
      activeOptionsProperty.id,
      activeOptionsDraft,
    );
    const previewMinerIndex = findBulkRenamePropertyPreviewMinerIndex(
      previewPreferences,
      activeOptionsProperty.id,
      previewMiners,
    );

    if (previewMinerIndex === null) {
      return emptyOptionsPreview;
    }

    return buildBulkRenamePropertyPreview(
      previewPreferences,
      activeOptionsProperty.id,
      previewMiners[previewMinerIndex],
      previewMiners[previewMinerIndex].counterIndex,
    );
  }, [activeOptionsDraft, activeOptionsProperty, preferences, previewMiners]);

  const previewRows = useMemo(() => getPreviewRows(previewMiners, previewNames), [previewMiners, previewNames]);
  const isBusy = isSubmitting;

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title="Update worker names"
        onDismiss={onDismiss}
        isBusy={isBusy}
        closeAriaLabel="Close update worker names"
        buttons={[
          {
            text: selectionCount === 1 ? "Apply to 1 miner" : `Apply to ${selectionCount} miners`,
            variant: variants.primary,
            onClick: () => void handleSubmit(),
            disabled: isBusy || isLoadingPreview,
            testId: "bulk-worker-name-save-button",
          },
        ]}
        primaryPane={
          <BulkRenamePropertyForm
            preferences={preferences}
            propertiesTitle="Worker name properties"
            onDragEnd={handleDragEnd}
            onOpenOptions={setActiveOptionsPropertyId}
            onToggleEnabled={handleToggleEnabled}
            onChangeSeparator={(separator) =>
              setBulkWorkerNamePreferences({
                ...preferences,
                separator,
              })
            }
            leadingContent={
              <Callout
                className="mb-3"
                intent="default"
                prefixIcon={<Info />}
                title="Worker names determine how miners appear in pool dashboards."
              />
            }
          />
        }
        secondaryPane={
          <BulkRenamePreviewPanel
            isLoadingPreview={isLoadingPreview}
            previewRows={previewRows}
            showPreviewEllipsis={showPreviewEllipsis}
          />
        }
      />

      <BulkRenameDialogs
        open={open}
        showDuplicateNamesWarning={showDuplicateNamesWarning}
        showNoChangesWarning={showNoChangesWarning}
        showOverwriteWarning={showOverwriteWarning}
        duplicateNamesDialogBody={duplicateNamesDialogBody}
        noChangesDialogBody={noChangesDialogBody}
        overwriteDialogTitle="Overwrite existing worker names?"
        overwriteDialogBody={overwriteDialogBody}
        onDismissDuplicateNames={() => setShowDuplicateNamesWarning(false)}
        onContinueDuplicateNames={handleDuplicateNamesContinue}
        onDismissNoChanges={() => setShowNoChangesWarning(false)}
        onContinueNoChanges={() => {
          setShowNoChangesWarning(false);
          onDismiss();
        }}
        onDismissOverwriteWarning={() => setShowOverwriteWarning(false)}
        onContinueOverwriteWarning={() => {
          setShowOverwriteWarning(false);
          submitWithAuthenticatedCredentials();
        }}
      />

      <BulkRenameOptionModals
        activeOptionsPropertyId={activeOptionsProperty?.id ?? null}
        activeOptionsPropertyOptions={activeOptionsProperty?.options ?? null}
        previewName={activeOptionsPreview.previewName}
        highlightedText={activeOptionsPreview.highlightedText}
        highlightStartIndex={activeOptionsPreview.highlightStartIndex}
        onDismiss={handleDismissOptions}
        onChange={setActiveOptionsDraft}
        onConfirm={handleUpdateOptions}
      />
    </>
  );
};

export default BulkWorkerNameModal;
