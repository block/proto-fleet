import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  closestCenter,
  DndContext,
  DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import FleetPoolRow from "./FleetPoolRow";
import PoolSelectionModal from "./PoolSelectionModal/PoolSelectionModal";
import { MiningPool } from "./types";
import { PoolConfig, PoolSlotSource } from "@/protoFleet/api/useMinerCommand";
import useMinerPoolAssignments from "@/protoFleet/api/useMinerPoolAssignments";
import usePools from "@/protoFleet/api/usePools";
import { Alert, DismissCircleDark, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import { MAX_POOLS } from "@/shared/components/MiningPools/constants";
import PageOverlay from "@/shared/components/PageOverlay";
import ProgressCircular from "@/shared/components/ProgressCircular";
const UNKNOWN_POOL_ID_PREFIX = "unknown-";

interface AssignedPoolData {
  poolId: string | undefined; // undefined when pool not in Fleet
  poolName: string; // Stored locally to avoid race conditions with miningPools lookup
  poolUrl: string;
  poolUsername: string;
}

interface PoolSelectionPageProps {
  open?: boolean;
  deviceIdentifiers: string[];
  numberOfMiners?: number; // Optional explicit count (for "all" mode with filters)
  currentDevice?: string | null; // Optional single device identifier (for single miner edit)
  onAssignPools: (poolConfig: PoolConfig) => Promise<void>;
  onDismiss: () => void;
}

const PoolSelectionPage = ({
  open,
  deviceIdentifiers,
  numberOfMiners: numberOfMinersOverride,
  currentDevice,
  onAssignPools,
  onDismiss: onCancel,
}: PoolSelectionPageProps) => {
  const isVisible = open ?? true;
  const [assignedPoolData, setAssignedPoolData] = useState<AssignedPoolData[]>([]);
  const [showSelectionModal, setShowSelectionModal] = useState(false);
  const [editingPoolIndex, setEditingPoolIndex] = useState<number | null>(null);
  const [testingPoolId, setTestingPoolId] = useState<string | null>(null);
  const [showConnectionCallout, setShowConnectionCallout] = useState(false);
  const [connectionError, setConnectionError] = useState(false);

  const showSuccessCallout = useMemo(
    () => showConnectionCallout && !testingPoolId && !connectionError,
    [showConnectionCallout, testingPoolId, connectionError],
  );

  const showErrorCallout = useMemo(
    () => showConnectionCallout && !testingPoolId && connectionError,
    [showConnectionCallout, testingPoolId, connectionError],
  );

  const { fetchPoolAssignments, isLoading: isLoadingAssignments } = useMinerPoolAssignments();
  const { miningPools, validatePool } = usePools(isVisible);
  const [isAssigning, setIsAssigning] = useState(false);

  const loadedDeviceRef = useRef<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  // Handle ESC key to dismiss the page (only when modal is not open)
  useEffect(() => {
    if (!isVisible) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !showSelectionModal) {
        onCancel();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [isVisible, onCancel, showSelectionModal]);

  // Reset internal state when hidden to mirror prior conditional-mount behavior.
  useEffect(() => {
    if (isVisible) {
      return;
    }

    loadedDeviceRef.current = null;
    setAssignedPoolData([]);
    setShowSelectionModal(false);
    setEditingPoolIndex(null);
    setTestingPoolId(null);
    setShowConnectionCallout(false);
    setConnectionError(false);
  }, [isVisible]);

  useEffect(() => {
    if (!isVisible) {
      return;
    }

    const deviceToLoad = currentDevice ?? (deviceIdentifiers.length === 1 ? deviceIdentifiers[0] : null);

    if (loadedDeviceRef.current === deviceToLoad) {
      return;
    }

    const isDeviceChange = loadedDeviceRef.current !== null;
    let isMounted = true;

    const loadExistingPoolAssignments = async () => {
      if (isDeviceChange) {
        setAssignedPoolData([]);
      }

      if (!deviceToLoad) {
        loadedDeviceRef.current = deviceToLoad;
        return;
      }

      const pools = await fetchPoolAssignments(deviceToLoad);
      if (!isMounted) return;

      const poolData: AssignedPoolData[] = pools.map((pool) => ({
        poolId: pool.poolId?.toString(),
        poolName: "",
        poolUrl: pool.url,
        poolUsername: pool.username,
      }));
      setAssignedPoolData(poolData);
      loadedDeviceRef.current = deviceToLoad;
    };

    loadExistingPoolAssignments();

    return () => {
      isMounted = false;
    };
  }, [isVisible, deviceIdentifiers, currentDevice, fetchPoolAssignments]);

  // Create a stable ID for each pool (either real poolId or synthetic for unknown pools)
  const getPoolDisplayId = useCallback((data: AssignedPoolData, index: number): string => {
    return data.poolId ?? `${UNKNOWN_POOL_ID_PREFIX}${index}`;
  }, []);

  // IDs for drag-and-drop context
  const sortableIds = useMemo(
    () => assignedPoolData.map((data, index) => getPoolDisplayId(data, index)),
    [assignedPoolData, getPoolDisplayId],
  );

  // Map assigned pool data to MiningPool objects for display
  const assignedPools = useMemo(
    () =>
      assignedPoolData.map((data, index): MiningPool => {
        // Use stored pool name if available (for newly created pools).
        // Otherwise look up from miningPools (for pools loaded from API).
        let name = data.poolName;
        if (!name && data.poolId) {
          const knownPool = miningPools.find((p) => p.poolId === data.poolId);
          if (knownPool) {
            name = knownPool.name;
          }
        }

        return {
          poolId: getPoolDisplayId(data, index),
          name: name || data.poolUrl,
          poolUrl: data.poolUrl,
          username: data.poolUsername,
        };
      }),
    [assignedPoolData, miningPools, getPoolDisplayId],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;

      if (over && active.id !== over.id) {
        setAssignedPoolData((items) => {
          const oldIndex = sortableIds.indexOf(active.id as string);
          const newIndex = sortableIds.indexOf(over.id as string);
          return arrayMove(items, oldIndex, newIndex);
        });
      }
    },
    [sortableIds],
  );

  const handleAddPool = useCallback(() => {
    setEditingPoolIndex(null);
    setShowSelectionModal(true);
    setShowConnectionCallout(false);
  }, []);

  const handleUpdatePool = useCallback((index: number) => {
    setEditingPoolIndex(index);
    setShowSelectionModal(true);
    setShowConnectionCallout(false);
  }, []);

  const handlePoolSelected = useCallback(
    (poolId: string, poolData?: MiningPool) => {
      // Use provided poolData (for newly created pools) or find from miningPools
      const selectedPool = poolData ?? miningPools.find((p) => p.poolId === poolId);
      if (!selectedPool) return;

      const newPoolData: AssignedPoolData = {
        poolId: poolId,
        poolName: selectedPool.name,
        poolUrl: selectedPool.poolUrl,
        poolUsername: selectedPool.username,
      };

      if (editingPoolIndex !== null) {
        setAssignedPoolData((prev) => {
          const newData = [...prev];
          newData[editingPoolIndex] = newPoolData;
          return newData;
        });
      } else {
        setAssignedPoolData((prev) => [...prev, newPoolData]);
      }
      setShowSelectionModal(false);
      setEditingPoolIndex(null);
      setShowConnectionCallout(false);
    },
    [editingPoolIndex, miningPools],
  );

  const handleRemovePool = useCallback(
    (displayId: string) => {
      const indexToRemove = sortableIds.indexOf(displayId);
      if (indexToRemove !== -1) {
        setAssignedPoolData((prev) => prev.filter((_, index) => index !== indexToRemove));
      }
    },
    [sortableIds],
  );

  const handleTestConnection = useCallback(
    (pool: MiningPool) => {
      if (testingPoolId) return;

      setTestingPoolId(pool.poolId);
      setConnectionError(false);
      validatePool({
        poolInfo: {
          url: pool.poolUrl,
          username: pool.username,
        },
        onSuccess: () => {
          setConnectionError(false);
        },
        onError: () => {
          setConnectionError(true);
        },
        onFinally: () => {
          setTestingPoolId(null);
          setShowConnectionCallout(true);
        },
      });
    },
    [testingPoolId, validatePool],
  );

  const handleAssignPoolsClick = async () => {
    if (assignedPoolData.length === 0) return;

    setIsAssigning(true);
    try {
      // Convert assigned pool data to PoolSlotSource objects
      const toPoolSlotSource = (data: AssignedPoolData): PoolSlotSource => {
        if (data.poolId) {
          return { type: "poolId", poolId: data.poolId };
        } else {
          return { type: "rawPool", url: data.poolUrl, username: data.poolUsername };
        }
      };

      const poolConfig: PoolConfig = {
        defaultPool: toPoolSlotSource(assignedPoolData[0]),
        backup1Pool: assignedPoolData[1] ? toPoolSlotSource(assignedPoolData[1]) : undefined,
        backup2Pool: assignedPoolData[2] ? toPoolSlotSource(assignedPoolData[2]) : undefined,
      };

      await onAssignPools(poolConfig);
    } catch (error) {
      console.error("Failed to assign pools:", error);
    } finally {
      setIsAssigning(false);
    }
  };

  const numberOfMiners = numberOfMinersOverride ?? deviceIdentifiers.length;
  const buttonText = `Assign to ${numberOfMiners} miner${numberOfMiners === 1 ? "" : "s"}`;
  const isSingleMinerEdit = numberOfMiners === 1;
  const isLoadingInitialState = isSingleMinerEdit && isLoadingAssignments;
  const hasConfiguredPools = assignedPoolData.length > 0;
  const canAddMorePools = assignedPoolData.length < MAX_POOLS;

  // Extract known pool IDs for modal exclusion (all assigned pools should be greyed out)
  const excludedPoolIds = assignedPoolData.map((data) => data.poolId).filter((id): id is string => id !== undefined);

  // Extract unknown pools (pools on miner but not in Fleet) for display in modal
  // Always show all unknown pools, even when editing one (they're disabled anyway)
  // Use consistent IDs that match getPoolDisplayId to avoid mismatches
  const unknownPoolsForModal = useMemo(
    () =>
      assignedPoolData
        .map((data, index) => ({ data, originalIndex: index }))
        .filter(({ data }) => data.poolId === undefined)
        .map(({ data, originalIndex }) => ({
          poolId: getPoolDisplayId(data, originalIndex),
          name: "—",
          poolUrl: data.poolUrl,
          username: data.poolUsername,
        })),
    [assignedPoolData, getPoolDisplayId],
  );

  // Check for duplicate URL+username combinations in assigned pools
  const hasDuplicatePools = useMemo(() => {
    if (assignedPoolData.length < 2) return false;

    const seen = new Set<string>();
    for (const pool of assignedPoolData) {
      const key = `${pool.poolUrl.trim().toLowerCase()}|${pool.poolUsername.trim().toLowerCase()}`;
      if (seen.has(key)) {
        return true;
      }
      seen.add(key);
    }
    return false;
  }, [assignedPoolData]);

  return (
    <PageOverlay open={open}>
      <div className="h-full w-full overflow-auto bg-surface-base px-6 pt-4 pb-6">
        <Header
          className="sticky top-0 z-10 pb-14"
          title="Assign pools"
          titleSize="text-heading-300"
          icon={
            <DismissCircleDark
              ariaLabel="Close assign pools"
              width="w-6"
              onClick={onCancel}
              testId="header-icon-button"
            />
          }
          inline
          buttons={[
            {
              text: buttonText,
              variant: variants.primary,
              onClick: handleAssignPoolsClick,
              disabled: !hasConfiguredPools || isLoadingInitialState || isAssigning || hasDuplicatePools,
              loading: isAssigning,
            },
          ]}
        />

        <div className="mx-auto max-w-[800px]">
          <div className="flex flex-col">
            {/* Page header */}
            <div className="mb-6 flex flex-col">
              <h1 className="text-heading-300 text-text-primary">Assign pools to your miner</h1>
              <p className="text-300 text-text-primary-70">
                Add up to 3 pools in order of priority. If a pool fails or is removed, Fleet switches to the next
                available pool automatically.
              </p>
            </div>

            {/* Connection test result callouts */}
            <DismissibleCalloutWrapper
              icon={<Success />}
              intent={intents.success}
              onDismiss={() => setShowConnectionCallout(false)}
              show={showSuccessCallout}
              title="Pool connection successful"
              testId="pool-selection-page-connection-success-callout"
            />
            <DismissibleCalloutWrapper
              icon={<Alert width={iconSizes.medium} />}
              intent={intents.danger}
              onDismiss={() => setShowConnectionCallout(false)}
              show={showErrorCallout}
              title="We couldn't connect with your pool. Review your pool details and try again."
              testId="pool-selection-page-connection-error-callout"
            />

            {/* Duplicate pools warning */}
            {hasDuplicatePools && (
              <Callout
                intent={intents.warning}
                prefixIcon={<Alert />}
                title="Duplicate pool configuration detected"
                subtitle="Two or more pools have the same URL and username. Please remove or change the duplicate pools before assigning."
              />
            )}

            {/* Pool list */}
            {isLoadingInitialState ? (
              <div className="flex flex-col items-center justify-center gap-3 py-16">
                <ProgressCircular size={32} indeterminate />
                <span className="text-text-secondary text-300">Loading pool configuration...</span>
              </div>
            ) : !hasConfiguredPools ? (
              // Empty state - just the Add pool button aligned left
              <div className="flex">
                <Button
                  text="Add pool"
                  variant={variants.secondary}
                  size={sizes.base}
                  onClick={handleAddPool}
                  testId="add-pool-button"
                />
              </div>
            ) : (
              // Pool list
              <div className="flex flex-col gap-4">
                <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
                  <SortableContext items={sortableIds} strategy={verticalListSortingStrategy}>
                    <div className="flex flex-col gap-2">
                      {assignedPools.map((pool, index) => (
                        <FleetPoolRow
                          key={pool.poolId}
                          pool={pool}
                          priorityNumber={index + 1}
                          onUpdate={() => handleUpdatePool(index)}
                          onTestConnection={() => handleTestConnection(pool)}
                          onRemove={() => handleRemovePool(pool.poolId)}
                          testId={`pool-row-${index}`}
                        />
                      ))}
                    </div>
                  </SortableContext>
                </DndContext>

                {canAddMorePools && (
                  <div className="flex">
                    <Button
                      text="Add another pool"
                      variant={variants.secondary}
                      size={sizes.base}
                      onClick={handleAddPool}
                      testId="add-another-pool-button"
                    />
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      <PoolSelectionModal
        open={isVisible && showSelectionModal}
        onDismiss={() => {
          setShowSelectionModal(false);
          setEditingPoolIndex(null);
        }}
        onSave={handlePoolSelected}
        excludedPoolIds={excludedPoolIds}
        unknownPools={unknownPoolsForModal}
      />
    </PageOverlay>
  );
};

export default PoolSelectionPage;
