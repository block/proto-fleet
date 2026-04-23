import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { type DragEndEvent } from "@dnd-kit/core";
import {
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
  mapSnapshotsToBulkRenamePreviewMiners,
  mapSnapshotToBulkRenamePreviewMiner,
  shouldShowBulkRenameNoChangesWarning,
  takePreviewMiners,
} from "./bulkRenamePreview";
import BulkRenamePreviewPanel, { type PreviewRow } from "./BulkRenamePreviewPanel";
import BulkRenamePropertyForm from "./BulkRenamePropertyForm";
import {
  getBulkRenameFailureMessage,
  getBulkRenameLoadingMessage,
  getBulkRenameRequestFailureMessage,
  getBulkRenameSuccessMessage,
} from "./bulkRenameToastMessages";
import {
  type CustomPropertyOptionsValues,
  type FixedValueOptionsValues,
  type QualifierOptionsValues,
} from "./RenameOptionsModals/types";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  type SortConfig,
  SortConfigSchema,
  SortDirection,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  DeviceSelectorSchema,
  type MinerListFilter,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useRenameMiners from "@/protoFleet/api/useRenameMiners";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import {
  applyFleetSelectablePairingStatuses,
  isFleetSelectablePairingStatus,
} from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { useAuthErrors, useBulkRenamePreferences, useSetBulkRenamePreferences } from "@/protoFleet/store";
import { variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface BulkRenameModalProps {
  open: boolean;
  selectedMinerIds: string[];
  selectionMode: SelectionMode;
  totalCount?: number;
  currentFilter?: MinerListFilter;
  currentSort?: SortConfig;
  miners: Record<string, MinerStateSnapshot>;
  minerIds: string[];
  onRefetchMiners?: () => void;
  onDismiss: () => void;
}

const duplicateNamesDialogBody =
  "Some miners may have duplicate names. Proceeding may impact accuracy in operations and reporting. Do you want to continue anyway?";
const noChangesDialogBody =
  "You can continue to retain your existing miner names, or keep editing. Do you want to continue anyway?";
const emptyOptionsPreview = {
  previewName: "",
  highlightedText: undefined,
  highlightStartIndex: undefined,
} as const;

const getSelectionCount = (selectionMode: SelectionMode, selectedMinerIds: string[], totalCount?: number): number => {
  if (selectionMode === "all") {
    return totalCount ?? selectedMinerIds.length;
  }

  return selectedMinerIds.length;
};

const computePreviewNames = (preferences: BulkRenamePreferences, previewMiners: BulkRenamePreviewMiner[]): string[] => {
  const config = buildBulkRenameConfig(preferences);
  return previewMiners.map((miner) => evaluateBulkRenamePreviewName(config, miner, miner.counterIndex));
};

const getPreviewRows = (previewMiners: BulkRenamePreviewMiner[], previewNames: string[]): PreviewRow[] =>
  previewMiners.map((miner, index) => ({
    currentName: miner.currentName,
    newName: previewNames[index] ?? "",
  }));

const buildOptionsPreviewPreferences = (
  preferences: BulkRenamePreferences,
  propertyId: BulkRenamePropertyId,
  options: BulkRenamePropertyOptions | null,
): BulkRenamePreferences =>
  updateBulkRenameProperty(preferences, propertyId, (property) => ({
    ...property,
    enabled: true,
    options: options ?? property.options,
  }));

const BulkRenameModal = ({
  open,
  selectedMinerIds,
  selectionMode,
  totalCount,
  currentFilter,
  currentSort,
  miners: minersById,
  minerIds,
  onRefetchMiners,
  onDismiss,
}: BulkRenameModalProps) => {
  const bulkRenamePreferences = useBulkRenamePreferences();
  const setBulkRenamePreferences = useSetBulkRenamePreferences();
  const { handleAuthErrors } = useAuthErrors();
  const { renameMiners } = useRenameMiners();
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
  const bulkRenamePreferencesRef = useRef(bulkRenamePreferences);
  const previewMinersRef = useRef(previewMiners);

  const selectionCount = useMemo(
    () => getSelectionCount(selectionMode, selectedMinerIds, totalCount),
    [selectionMode, selectedMinerIds, totalCount],
  );
  const previewSampleSize = useMemo(() => (isPhone || isTablet ? 1 : 6), [isPhone, isTablet]);
  const selectedMinerIdSet = useMemo(() => new Set(selectedMinerIds), [selectedMinerIds]);

  const localPreviewMiners = useMemo(() => {
    if (selectionMode === "subset") {
      return mapSnapshotsToBulkRenamePreviewMiners(
        minerIds
          .filter((deviceIdentifier) => selectedMinerIdSet.has(deviceIdentifier))
          .map((deviceIdentifier) => minersById[deviceIdentifier])
          .filter((miner): miner is NonNullable<typeof miner> => miner !== undefined),
      );
    }

    return mapSnapshotsToBulkRenamePreviewMiners(
      minerIds
        .map((deviceIdentifier) => minersById[deviceIdentifier])
        .filter(
          (miner): miner is NonNullable<typeof miner> =>
            miner !== undefined && isFleetSelectablePairingStatus(miner.pairingStatus),
        ),
    );
  }, [minerIds, minersById, selectedMinerIdSet, selectionMode]);

  const localValidationMiners = useMemo(() => {
    if (selectionMode === "subset") {
      return localPreviewMiners.length === selectedMinerIds.length ? localPreviewMiners : null;
    }

    return localPreviewMiners.length === selectionCount ? localPreviewMiners : null;
  }, [localPreviewMiners, selectedMinerIds.length, selectionCount, selectionMode]);

  const loadPreviewMiners = useCallback(async (): Promise<{
    miners: BulkRenamePreviewMiner[];
    showEllipsis: boolean;
  }> => {
    if (selectionMode === "subset") {
      return takePreviewMiners(localPreviewMiners, localPreviewMiners.length, previewSampleSize);
    }

    if (previewSampleSize === 1) {
      const filter = applyFleetSelectablePairingStatuses(currentFilter);
      const sort = currentSort ? [currentSort] : [];
      const response = await fleetManagementClient.listMinerStateSnapshots({
        pageSize: 1,
        filter,
        sort,
      });

      return {
        miners: mapSnapshotsToBulkRenamePreviewMiners(response.miners),
        showEllipsis: false,
      };
    }

    if (localValidationMiners !== null) {
      return takePreviewMiners(localValidationMiners, selectionCount, previewSampleSize);
    }

    const filter = applyFleetSelectablePairingStatuses(currentFilter);
    const sort = currentSort ? [currentSort] : [];
    const reverseSort = currentSort
      ? [
          create(SortConfigSchema, {
            field: currentSort.field,
            direction: currentSort.direction === SortDirection.DESC ? SortDirection.ASC : SortDirection.DESC,
          }),
        ]
      : [
          create(SortConfigSchema, {
            field: SortField.NAME,
            direction: SortDirection.DESC,
          }),
        ];

    if (selectionCount <= previewSampleSize) {
      const response = await fleetManagementClient.listMinerStateSnapshots({
        pageSize: selectionCount,
        filter,
        sort,
      });

      return {
        miners: mapSnapshotsToBulkRenamePreviewMiners(response.miners),
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
        ...firstResponse.miners.map((miner, index) => mapSnapshotToBulkRenamePreviewMiner(miner, index)),
        ...lastResponse.miners
          .map((miner, index) => mapSnapshotToBulkRenamePreviewMiner(miner, selectionCount - index - 1))
          .reverse(),
      ],
      showEllipsis: true,
    };
  }, [
    currentFilter,
    currentSort,
    localValidationMiners,
    localPreviewMiners,
    previewSampleSize,
    selectionCount,
    selectionMode,
  ]);

  useEffect(() => {
    if (!open) {
      setActiveOptionsPropertyId(null);
      setActiveOptionsDraft(null);
      setShowDuplicateNamesWarning(false);
      setShowNoChangesWarning(false);
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
    bulkRenamePreferencesRef.current = bulkRenamePreferences;
  }, [bulkRenamePreferences]);

  useEffect(() => {
    previewMinersRef.current = previewMiners;
  }, [previewMiners]);

  useEffect(() => {
    if (!open) {
      return;
    }

    setPreviewNames(computePreviewNames(bulkRenamePreferencesRef.current, previewMiners));
  }, [open, previewMiners]);

  useEffect(() => {
    if (!open) {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setPreviewNames(computePreviewNames(bulkRenamePreferences, previewMinersRef.current));
    }, 500);

    return () => {
      window.clearTimeout(timeoutId);
    };
  }, [bulkRenamePreferences, open]);

  const handleToggleEnabled = useCallback(
    (propertyId: BulkRenamePropertyId, enabled: boolean) => {
      setBulkRenamePreferences(
        updateBulkRenameProperty(bulkRenamePreferences, propertyId, (property) => ({
          ...property,
          enabled,
        })),
      );
    },
    [bulkRenamePreferences, setBulkRenamePreferences],
  );

  const handleUpdateOptions = useCallback(
    (
      propertyId: BulkRenamePropertyId,
      options: CustomPropertyOptionsValues | FixedValueOptionsValues | QualifierOptionsValues,
    ) => {
      setBulkRenamePreferences(
        updateBulkRenameProperty(bulkRenamePreferences, propertyId, (property) => ({
          ...property,
          options,
        })),
      );
      setActiveOptionsDraft(null);
      setActiveOptionsPropertyId(null);
    },
    [bulkRenamePreferences, setBulkRenamePreferences],
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

      setBulkRenamePreferences(
        reorderBulkRenameProperties(
          bulkRenamePreferences,
          active.id as BulkRenamePropertyId,
          over.id as BulkRenamePropertyId,
        ),
      );
    },
    [bulkRenamePreferences, setBulkRenamePreferences],
  );

  const proceedWithSubmit = useCallback(async () => {
    const allDevicesFilter = applyFleetSelectablePairingStatuses(currentFilter);
    const config = buildBulkRenameConfig(bulkRenamePreferences);

    const deviceSelector = create(DeviceSelectorSchema, {
      selectionType:
        selectionMode === "all"
          ? {
              case: "allDevices",
              value: allDevicesFilter,
            }
          : {
              case: "includeDevices",
              value: create(DeviceIdentifierListSchema, {
                deviceIdentifiers: selectedMinerIds,
              }),
            },
    });

    const toastId = pushToast({
      message: getBulkRenameLoadingMessage(selectionCount),
      status: TOAST_STATUSES.loading,
      longRunning: true,
    });

    setIsSubmitting(true);

    try {
      const response = await renameMiners(deviceSelector, config, currentSort);
      onRefetchMiners?.();

      if (response.renamedCount > 0 || response.unchangedCount > 0) {
        updateToast(toastId, {
          message: getBulkRenameSuccessMessage(response.renamedCount, response.unchangedCount),
          status: TOAST_STATUSES.success,
        });
      } else {
        removeToast(toastId);
      }

      if (response.failedCount > 0) {
        pushToast({
          message: getBulkRenameFailureMessage(response.failedCount),
          status: TOAST_STATUSES.error,
          longRunning: true,
        });
      }

      onDismiss();
    } catch {
      updateToast(toastId, {
        message: getBulkRenameRequestFailureMessage(selectionCount),
        status: TOAST_STATUSES.error,
      });
    } finally {
      setIsSubmitting(false);
    }
  }, [
    bulkRenamePreferences,
    currentFilter,
    onDismiss,
    onRefetchMiners,
    renameMiners,
    currentSort,
    selectedMinerIds,
    selectionCount,
    selectionMode,
  ]);

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
    () => shouldShowBulkRenameNoChangesWarning(bulkRenamePreferences, noChangeValidationMiners),
    [bulkRenamePreferences, noChangeValidationMiners],
  );

  const handleSubmit = useCallback(() => {
    // The visible preview is capped to a small head/tail sample for large selections. We only show the no-change
    // dialog when we can validate against the full selection from data already in memory; otherwise we avoid extra
    // miner-loading API calls in the UI and let the backend handle the bulk rename request.
    if (shouldShowNoChangesWarning) {
      setShowNoChangesWarning(true);
      return;
    }

    if (shouldWarnAboutBulkRenameDuplicates(selectionCount, bulkRenamePreferences, noChangeValidationMiners)) {
      setShowDuplicateNamesWarning(true);
      return;
    }

    void proceedWithSubmit();
  }, [bulkRenamePreferences, noChangeValidationMiners, proceedWithSubmit, selectionCount, shouldShowNoChangesWarning]);

  const handleDuplicateNamesContinue = useCallback(() => {
    setShowDuplicateNamesWarning(false);

    if (shouldShowNoChangesWarning) {
      setShowNoChangesWarning(true);
      return;
    }

    void proceedWithSubmit();
  }, [proceedWithSubmit, shouldShowNoChangesWarning]);

  const activeOptionsProperty = useMemo(
    () => bulkRenamePreferences.properties.find((property) => property.id === activeOptionsPropertyId) ?? null,
    [activeOptionsPropertyId, bulkRenamePreferences.properties],
  );

  const activeOptionsPreview = useMemo(() => {
    if (activeOptionsProperty === null || previewMiners.length === 0) {
      return emptyOptionsPreview;
    }

    const previewPreferences = buildOptionsPreviewPreferences(
      bulkRenamePreferences,
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
  }, [activeOptionsDraft, activeOptionsProperty, bulkRenamePreferences, previewMiners]);

  const previewRows = useMemo(() => getPreviewRows(previewMiners, previewNames), [previewMiners, previewNames]);
  const isBusy = isSubmitting;

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title="Rename miners"
        onDismiss={onDismiss}
        isBusy={isBusy}
        closeAriaLabel="Close rename miners"
        buttons={[
          {
            text: selectionCount === 1 ? "Apply to 1 miner" : `Apply to ${selectionCount} miners`,
            variant: variants.primary,
            onClick: () => void handleSubmit(),
            disabled: isBusy || isLoadingPreview,
            testId: "bulk-rename-save-button",
          },
        ]}
        primaryPane={
          <BulkRenamePropertyForm
            preferences={bulkRenamePreferences}
            onDragEnd={handleDragEnd}
            onOpenOptions={setActiveOptionsPropertyId}
            onToggleEnabled={handleToggleEnabled}
            onChangeSeparator={(separator) =>
              setBulkRenamePreferences({
                ...bulkRenamePreferences,
                separator,
              })
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
        duplicateNamesDialogBody={duplicateNamesDialogBody}
        noChangesDialogBody={noChangesDialogBody}
        onDismissDuplicateNames={() => setShowDuplicateNamesWarning(false)}
        onContinueDuplicateNames={handleDuplicateNamesContinue}
        onDismissNoChanges={() => setShowNoChangesWarning(false)}
        onContinueNoChanges={() => {
          setShowNoChangesWarning(false);
          onDismiss();
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

export default BulkRenameModal;
