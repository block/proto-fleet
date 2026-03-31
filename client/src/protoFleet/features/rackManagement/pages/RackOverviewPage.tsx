import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";

import type {
  CollectionStats,
  DeviceCollection,
  RackCoolingType,
  RackOrderIndex,
} from "@/protoFleet/api/generated/collection/v1/collection_pb";
import {
  AggregationType,
  GetCombinedMetricsResponse,
  MeasurementType,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useCollections } from "@/protoFleet/api/useCollections";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import CollectionActionsMenu from "@/protoFleet/features/groupManagement/components/CollectionActionsMenu";
import { CollectionPerformanceSection } from "@/protoFleet/features/groupManagement/components/CollectionPerformanceSection";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import {
  AssignMinersModal,
  type RackFormData,
} from "@/protoFleet/features/rackManagement/components/AssignMinersModal";
import { orderIndexToOrigin } from "@/protoFleet/features/rackManagement/components/AssignMinersModal/types";
import type { SlotHealthState } from "@/protoFleet/features/rackManagement/components/RackDetailGrid/types";
import { RackHealthModule } from "@/protoFleet/features/rackManagement/components/RackHealthModule";
import { SLOT_STATUS_MAP } from "@/protoFleet/features/rackManagement/utils/rackCardMapper";
import {
  useAppendStreamingMetrics,
  useAppendStreamingTemperatureCounts,
  useAppendStreamingUptimeCounts,
  useClearMetrics,
  useDuration,
  useMinerStateCounts,
  useSetAllHistoricalData,
  useSetDashboardError,
  useSetDuration,
  useSetMinerStateCounts,
} from "@/protoFleet/store";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useNavigate } from "@/shared/hooks/useNavigate";
import { useStickyState } from "@/shared/hooks/useStickyState";

const RACK_OVERVIEW_POLL_INTERVAL_MS = Number(import.meta.env.VITE_RACK_OVERVIEW_POLL_INTERVAL_MS) || 60000;

const ALL_MEASUREMENT_TYPES: MeasurementType[] = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
  MeasurementType.UPTIME,
];

const ALL_AGGREGATION_TYPES: AggregationType[] = [AggregationType.AVERAGE, AggregationType.MIN, AggregationType.MAX];

const RackOverviewPage = () => {
  const { rackId: rackIdParam } = useParams<{ rackId: string }>();
  const navigate = useNavigate();

  // Rack resolution state
  const [rack, setRack] = useState<DeviceCollection | null>(null);
  const [memberDeviceIds, setMemberDeviceIds] = useState<string[] | null>(null);
  const [collectionStats, setCollectionStats] = useState<CollectionStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [showEditModal, setShowEditModal] = useState(false);
  const sleepActionRef = useRef<(() => void) | null>(null);
  const actionActiveRef = useRef(false);

  const { getCollection, listGroupMembers, getCollectionStats } = useCollections();

  // Request versioning to guard against stale resolution callbacks
  const resolveVersionRef = useRef(0);

  // Resolve rack by ID → set rack + member device IDs + stats
  // When `silent` is true (polling), keep existing state visible while refreshing in the background.
  const resolveRack = useCallback(
    (rackId: bigint, { silent = false } = {}) => {
      const version = ++resolveVersionRef.current;
      if (!silent) {
        setLoading(true);
        setRack(null);
        setMemberDeviceIds(null);
        setCollectionStats(null);
        setNotFound(false);
        setResolveError(null);
      }

      getCollection({
        collectionId: rackId,
        onSuccess: (collection) => {
          if (version !== resolveVersionRef.current) return;

          // Reject non-rack collections
          if (collection.typeDetails.case !== "rackInfo") {
            setNotFound(true);
            setLoading(false);
            return;
          }

          setRack(collection);
          // Clear any latched error state from a prior failed poll
          setNotFound(false);
          setResolveError(null);

          // Wait for both members and stats before clearing loading state
          let pending = 2;
          const onRequestDone = () => {
            pending--;
            if (pending <= 0) setLoading(false);
          };

          // Fetch member device IDs
          listGroupMembers({
            collectionId: collection.id,
            onSuccess: (deviceIdentifiers) => {
              if (version !== resolveVersionRef.current) return;
              // Only update if membership actually changed to avoid resetting telemetry
              setMemberDeviceIds((prev) => {
                if (
                  prev &&
                  prev.length === deviceIdentifiers.length &&
                  prev.every((id, i) => id === deviceIdentifiers[i])
                ) {
                  return prev;
                }
                return deviceIdentifiers;
              });
              onRequestDone();
            },
            onError: (msg) => {
              if (version !== resolveVersionRef.current) return;
              if (!silent) {
                setResolveError(msg);
              }
              onRequestDone();
            },
          });

          // Fetch collection stats (for slot grid + KPIs)
          getCollectionStats({
            collectionIds: [collection.id],
            onSuccess: (stats) => {
              if (version !== resolveVersionRef.current) return;
              if (stats.length > 0) {
                setCollectionStats(stats[0]);
              }
              onRequestDone();
            },
            onError: () => {
              if (version !== resolveVersionRef.current) return;
              onRequestDone();
            },
          });
        },
        onNotFound: () => {
          if (version !== resolveVersionRef.current) return;
          setNotFound(true);
          setLoading(false);
        },
        onError: (msg) => {
          if (version !== resolveVersionRef.current) return;
          // During silent polls, don't latch errors — keep existing UI visible
          if (silent) return;
          setResolveError(msg);
          setLoading(false);
        },
      });
    },
    [getCollection, listGroupMembers, getCollectionStats],
  );

  // Initial resolution from URL param
  useEffect(() => {
    if (!rackIdParam) {
      setNotFound(true);
      setLoading(false);
      return;
    }

    try {
      const id = BigInt(rackIdParam);
      resolveRack(id);
    } catch {
      setNotFound(true);
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rackIdParam]);

  // Polling — refresh rack data, paused while modals or bulk-action dialogs are open
  useEffect(() => {
    if (loading || !rack || showEditModal) return;
    const intervalId = setInterval(() => {
      if (actionActiveRef.current) return;
      resolveRack(rack.id, { silent: true });
    }, RACK_OVERVIEW_POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [loading, rack, showEditModal, resolveRack]);

  // Rack metadata
  const rackInfo = rack?.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
  const rows = rackInfo?.rows ?? 1;
  const cols = rackInfo?.columns ?? 1;
  const orderIndex = rackInfo?.orderIndex;
  const numberingOrigin = orderIndex !== undefined ? orderIndexToOrigin(orderIndex) : "bottom-left";

  // Build slot states for RackDetailGrid from collection stats
  const slotStates = useMemo<Record<string, SlotHealthState>>(() => {
    if (!collectionStats) return {};
    const states: Record<string, SlotHealthState> = {};
    for (const s of collectionStats.slotStatuses) {
      states[`${s.row}-${s.column}`] = SLOT_STATUS_MAP[s.status] ?? "empty";
    }
    return states;
  }, [collectionStats]);

  // AssignMinersModal form data (for edit rack flow)
  const assignMinersFormData = useMemo<RackFormData | null>(() => {
    if (!showEditModal || !rack || !rackInfo) return null;
    return {
      label: rack.label,
      zone: rackInfo.zone ?? "",
      rows: rackInfo.rows ?? 1,
      columns: rackInfo.columns ?? 1,
      orderIndex: rackInfo.orderIndex as RackOrderIndex,
      coolingType: rackInfo.coolingType as RackCoolingType,
    };
  }, [showEditModal, rack, rackInfo]);

  // Dashboard store hooks
  const duration = useDuration();
  const setDuration = useSetDuration();
  const { refs } = useStickyState();

  // Component errors scoped to rack's devices
  const componentErrorsOptions = useMemo(
    () => (memberDeviceIds ? { deviceIdentifiers: memberDeviceIds } : undefined),
    [memberDeviceIds],
  );
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors(componentErrorsOptions);

  const stateCounts = useMinerStateCounts();
  const setMinerStateCounts = useSetMinerStateCounts();

  // Store action hooks
  const setAllHistoricalData = useSetAllHistoricalData();
  const appendStreamingMetrics = useAppendStreamingMetrics();
  const appendStreamingTemperatureCounts = useAppendStreamingTemperatureCounts();
  const appendStreamingUptimeCounts = useAppendStreamingUptimeCounts();
  const clearMetrics = useClearMetrics();
  const setError = useSetDashboardError();

  // Clear dashboard store on mount and unmount
  useEffect(() => {
    clearMetrics();
    setMinerStateCounts(undefined);
    return () => {
      clearMetrics();
      setMinerStateCounts(undefined);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Telemetry fetching - scoped to rack's device IDs
  const telemetryEnabled = memberDeviceIds !== null && memberDeviceIds.length > 0;
  const isEmptyRack = memberDeviceIds !== null && memberDeviceIds.length === 0;

  const telemetryOptions = useMemo(
    () => ({
      deviceIds: memberDeviceIds ?? [],
      measurementTypes: ALL_MEASUREMENT_TYPES,
      aggregations: ALL_AGGREGATION_TYPES,
      duration,
      enabled: telemetryEnabled,
    }),
    [memberDeviceIds, duration, telemetryEnabled],
  );

  const { data: historicalData, error } = useTelemetryMetrics(telemetryOptions);

  const streamingOptions = useMemo(
    () => ({
      deviceIds: memberDeviceIds ?? [],
      measurementTypes: ALL_MEASUREMENT_TYPES,
      aggregations: ALL_AGGREGATION_TYPES,
      enabled: telemetryEnabled,
    }),
    [memberDeviceIds, telemetryEnabled],
  );

  const { latestData: streamingData } = useStreamingTelemetryMetrics(streamingOptions);

  // Write historical data to store
  const lastLoadedDataRef = useRef<GetCombinedMetricsResponse | null>(null);
  const hasLoadedForCurrentDurationRef = useRef(false);

  useEffect(() => {
    if (!historicalData) return;
    if (historicalData === lastLoadedDataRef.current) return;
    if (hasLoadedForCurrentDurationRef.current) return;

    lastLoadedDataRef.current = historicalData;
    hasLoadedForCurrentDurationRef.current = true;
    setAllHistoricalData(
      historicalData.metrics ?? [],
      historicalData.temperatureStatusCounts ?? [],
      historicalData.uptimeStatusCounts ?? [],
    );
  }, [historicalData, setAllHistoricalData]);

  // Write error state to store
  useEffect(() => {
    setError(error ?? null);
  }, [error, setError]);

  // Clear metrics on duration change
  const prevDurationRef = useRef<typeof duration | undefined>(undefined);
  useEffect(() => {
    if (prevDurationRef.current !== undefined && prevDurationRef.current !== duration) {
      clearMetrics();
      hasLoadedForCurrentDurationRef.current = false;
    }
    prevDurationRef.current = duration;
  }, [duration, clearMetrics]);

  // Reset historical data refs and miner state counts when rack membership changes
  useEffect(() => {
    lastLoadedDataRef.current = null;
    hasLoadedForCurrentDurationRef.current = false;
    clearMetrics();
    setMinerStateCounts(undefined);
  }, [memberDeviceIds, clearMetrics, setMinerStateCounts]);

  // Seed empty metrics for zero-member racks (also re-seed after duration changes clear metrics)
  useEffect(() => {
    if (memberDeviceIds !== null && memberDeviceIds.length === 0) {
      setAllHistoricalData([], [], []);
    }
  }, [memberDeviceIds, duration, setAllHistoricalData]);

  // Append streaming data
  useEffect(() => {
    if (!streamingData) return;

    appendStreamingMetrics(streamingData.metrics ?? []);
    appendStreamingTemperatureCounts(streamingData.temperatureStatusCounts ?? []);
    appendStreamingUptimeCounts(streamingData.uptimeStatusCounts ?? []);
    setMinerStateCounts(streamingData.minerStateCounts);
  }, [
    streamingData,
    appendStreamingMetrics,
    appendStreamingTemperatureCounts,
    appendStreamingUptimeCounts,
    setMinerStateCounts,
  ]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (notFound) {
    return (
      <div className="p-10 phone:p-6 tablet:p-6">
        <h1 className="text-heading-300 text-text-primary">Rack not found</h1>
        <p className="mt-2 text-300 text-text-primary-50">No rack with ID &ldquo;{rackIdParam}&rdquo; exists.</p>
      </div>
    );
  }

  if (resolveError) {
    return (
      <div className="p-10 phone:p-6 tablet:p-6">
        <h1 className="text-heading-300 text-text-primary">Error loading rack</h1>
        <p className="mt-2 text-300 text-text-primary-50">{resolveError}</p>
      </div>
    );
  }

  return (
    <div className="h-full">
      <div className="flex flex-col">
        {/* Header */}
        <div className="p-10 pb-0 phone:p-6 phone:pb-0 tablet:p-6 tablet:pb-0">
          <Header
            title={rack?.label ?? ""}
            titleSize="text-heading-300"
            inline
            icon={<ChevronDown className="rotate-90" />}
            iconOnClick={() => navigate("/racks")}
          >
            <div className="ml-3 flex items-center gap-3">
              <Button variant={variants.secondary} onClick={() => navigate(`/miners?rack=${rack?.id}`)}>
                View miners
              </Button>
              <Button
                variant={variants.secondary}
                onClick={() => sleepActionRef.current?.()}
                disabled={!memberDeviceIds || memberDeviceIds.length === 0}
              >
                Sleep all miners
              </Button>
              <Button variant={variants.secondary} onClick={() => setShowEditModal(true)}>
                Edit rack
              </Button>
              <CollectionActionsMenu
                memberDeviceIds={memberDeviceIds ?? []}
                onEdit={() => setShowEditModal(true)}
                editLabel="Edit rack"
                onActionComplete={() => rack && resolveRack(rack.id)}
                sleepActionRef={sleepActionRef}
                actionActiveRef={actionActiveRef}
              />
            </div>
          </Header>
        </div>

        {/* Health Overview Section */}
        <section className="p-10 phone:p-6 tablet:p-6">
          <div className="flex flex-col gap-1">
            <RackHealthModule
              rows={rows}
              cols={cols}
              slotStates={slotStates}
              numberingOrigin={numberingOrigin}
              onEmptySlotClick={() => setShowEditModal(true)}
              hashingCount={stateCounts?.hashingCount ?? (isEmptyRack ? 0 : undefined)}
              needsAttentionCount={stateCounts?.brokenCount ?? (isEmptyRack ? 0 : undefined)}
              offlineCount={stateCounts?.offlineCount ?? (isEmptyRack ? 0 : undefined)}
              sleepingCount={stateCounts?.sleepingCount ?? (isEmptyRack ? 0 : undefined)}
              rackFilterParam={rack ? `rack=${rack.id}` : undefined}
            />
            <FleetErrors
              controlBoardErrors={controlBoardErrors}
              fanErrors={fanErrors}
              hashboardErrors={hashboardErrors}
              psuErrors={psuErrors}
              extraFilterParams={rack ? `rack=${rack.id}` : undefined}
            />
          </div>
        </section>

        {/* Performance Section */}
        <section className="pb-6">
          <div ref={refs.vertical.start} />
          <div className="sticky top-0 z-2 bg-surface-5 px-10 pt-10 pb-6 dark:bg-surface-base phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-heading-200 text-text-primary">Performance</div>
              <div className="flex items-center gap-6 text-200 text-core-primary-50">
                <div className="flex items-center gap-2">
                  <svg width="24" height="4">
                    <line
                      x1="0"
                      y1="2"
                      x2="24"
                      y2="2"
                      stroke="var(--color-core-primary-fill)"
                      strokeWidth="3"
                      strokeLinecap="round"
                    />
                  </svg>
                  <span>Rack</span>
                </div>
                <div className="flex items-center gap-2">
                  <svg width="24" height="4">
                    <line
                      x1="0"
                      y1="2"
                      x2="24"
                      y2="2"
                      stroke="var(--color-core-primary-50)"
                      strokeWidth="3"
                      strokeLinecap="round"
                      strokeDasharray="1 6"
                      strokeOpacity="0.5"
                    />
                  </svg>
                  <span>Max</span>
                </div>
                <div className="flex items-center gap-2">
                  <svg width="24" height="4">
                    <line
                      x1="0"
                      y1="2"
                      x2="24"
                      y2="2"
                      stroke="var(--color-intent-critical-fill)"
                      strokeWidth="3"
                      strokeLinecap="round"
                      strokeDasharray="1 6"
                      strokeOpacity="0.5"
                    />
                  </svg>
                  <span>Min</span>
                </div>
              </div>
              <div className="flex items-center">
                <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
              </div>
            </div>
          </div>

          <div className="px-10 phone:px-6 tablet:px-6">
            <CollectionPerformanceSection duration={duration} />
          </div>
          <div ref={refs.vertical.end} />
        </section>
      </div>

      {showEditModal && rack && assignMinersFormData && (
        <AssignMinersModal
          show
          rackSettings={assignMinersFormData}
          existingRackId={rack.id}
          existingRacks={[rack]}
          onDismiss={() => setShowEditModal(false)}
          onSave={() => {
            setShowEditModal(false);
            resolveRack(rack.id);
          }}
        />
      )}
    </div>
  );
};

export default RackOverviewPage;
