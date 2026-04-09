import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  AggregationType,
  GetCombinedMetricsResponse,
  MeasurementType,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import useDeviceSetStateCounts from "@/protoFleet/api/useDeviceSetStateCounts";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import FleetHealth from "@/protoFleet/features/dashboard/components/FleetHealth";
import DeviceSetActionsMenu from "@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import GroupModal from "@/protoFleet/features/groupManagement/components/GroupModal";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
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

const ALL_MEASUREMENT_TYPES: MeasurementType[] = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
  MeasurementType.UPTIME,
];

const ALL_AGGREGATION_TYPES: AggregationType[] = [AggregationType.AVERAGE, AggregationType.MIN, AggregationType.MAX];

const GroupOverviewPage = () => {
  const { groupLabel } = useParams<{ groupLabel: string }>();
  const label = groupLabel ?? "";
  const navigate = useNavigate();

  // Group resolution state
  const [group, setGroup] = useState<DeviceSet | null>(null);
  const [memberDeviceIds, setMemberDeviceIds] = useState<string[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [showEditModal, setShowEditModal] = useState(false);

  const { listGroups, listGroupMembers } = useDeviceSets();

  // Request versioning to guard against stale resolution callbacks
  const resolveVersionRef = useRef(0);

  // Resolve a group by label (or by ID if provided) → set group + member device IDs
  const resolveGroup = useCallback(
    (resolveLabel: string, groupId?: bigint) => {
      const version = ++resolveVersionRef.current;
      setLoading(true);
      setGroup(null);
      setMemberDeviceIds(null);
      setNotFound(false);
      setResolveError(null);

      listGroups({
        onSuccess: (deviceSets) => {
          if (version !== resolveVersionRef.current) return;
          const match = groupId
            ? deviceSets.find((c) => c.id === groupId)
            : deviceSets.find((c) => c.label === resolveLabel);
          if (!match) {
            setNotFound(true);
            setLoading(false);
            return;
          }
          setGroup(match);
          // If the label changed (e.g., after edit), navigate to the new URL
          if (match.label !== resolveLabel) {
            navigate(`/groups/${encodeURIComponent(match.label)}`);
            return;
          }
          listGroupMembers({
            deviceSetId: match.id,
            onSuccess: (deviceIdentifiers) => {
              if (version !== resolveVersionRef.current) return;
              setMemberDeviceIds(deviceIdentifiers);
              setLoading(false);
            },
            onError: (msg) => {
              if (version !== resolveVersionRef.current) return;
              setResolveError(msg);
              setLoading(false);
            },
          });
        },
        onError: (msg) => {
          if (version !== resolveVersionRef.current) return;
          setResolveError(msg);
          setLoading(false);
        },
      });
    },
    [listGroups, listGroupMembers, navigate],
  );

  // Resolve group label → group object → device IDs
  useEffect(() => {
    if (!label) {
      setNotFound(true);
      setLoading(false);
      return;
    }

    setLoading(true);
    resolveGroup(label);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [label]);

  // Dashboard store hooks
  const duration = useDuration();
  const setDuration = useSetDuration();
  const { refs } = useStickyState();

  // Component errors scoped to group's devices
  // Pass undefined when no members yet (loading); pass empty array for truly empty groups
  // so useComponentErrors can distinguish "no scope" from "empty scope"
  const componentErrorsOptions = useMemo(
    () => (memberDeviceIds ? { deviceIdentifiers: memberDeviceIds } : undefined),
    [memberDeviceIds],
  );
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors(componentErrorsOptions);

  // Group size for "X of Y miners reporting" subtitles
  const groupSize = memberDeviceIds?.length ?? 0;

  // Initial state counts scoped to this group (fallback until streaming delivers)
  // eslint-disable-next-line react-hooks/exhaustive-deps -- Intentionally keyed on group.id to avoid re-fetches when silent polling replaces the group object
  const stateCountsFilter = useMemo(() => (group ? { groupIds: [group.id] } : null), [group?.id]);
  const {
    totalMiners: initialTotalMiners,
    stateCounts: initialStateCounts,
    hasInitialLoadCompleted,
  } = useDeviceSetStateCounts(stateCountsFilter);

  const streamingStateCounts = useMinerStateCounts();
  const setMinerStateCounts = useSetMinerStateCounts();

  // Use streaming counts when available, fall back to initial scoped counts
  const stateCounts = streamingStateCounts ?? initialStateCounts;
  const totalMiners = streamingStateCounts
    ? (streamingStateCounts.hashingCount ?? 0) +
      (streamingStateCounts.brokenCount ?? 0) +
      (streamingStateCounts.offlineCount ?? 0) +
      (streamingStateCounts.sleepingCount ?? 0)
    : hasInitialLoadCompleted
      ? initialTotalMiners
      : groupSize;

  // Store action hooks
  const setAllHistoricalData = useSetAllHistoricalData();
  const appendStreamingMetrics = useAppendStreamingMetrics();
  const appendStreamingTemperatureCounts = useAppendStreamingTemperatureCounts();
  const appendStreamingUptimeCounts = useAppendStreamingUptimeCounts();
  const clearMetrics = useClearMetrics();
  const setError = useSetDashboardError();

  // Clear dashboard store on mount and unmount
  // Also reset minerStateCounts so stale all-fleet counts from Dashboard don't show
  useEffect(() => {
    clearMetrics();
    setMinerStateCounts(undefined);
    return () => {
      clearMetrics();
      setMinerStateCounts(undefined);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Telemetry fetching - scoped to group's device IDs
  const telemetryEnabled = memberDeviceIds !== null && memberDeviceIds.length > 0;
  const isEmptyGroup = memberDeviceIds !== null && memberDeviceIds.length === 0;

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

  // Write historical data to store (mirrors Dashboard pattern)
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

  // Reset historical data refs and miner state counts when group membership changes
  useEffect(() => {
    lastLoadedDataRef.current = null;
    hasLoadedForCurrentDurationRef.current = false;
    clearMetrics();
    setMinerStateCounts(undefined);
  }, [memberDeviceIds, clearMetrics, setMinerStateCounts]);

  // Seed empty metrics for zero-member groups so charts show "No data" instead of skeleton
  useEffect(() => {
    if (memberDeviceIds !== null && memberDeviceIds.length === 0) {
      setAllHistoricalData([], [], []);
    }
  }, [memberDeviceIds, setAllHistoricalData]);

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
        <h1 className="text-heading-300 text-text-primary">Group not found</h1>
        <p className="mt-2 text-300 text-text-primary-50">No group with the label &ldquo;{label}&rdquo; exists.</p>
      </div>
    );
  }

  if (resolveError) {
    return (
      <div className="p-10 phone:p-6 tablet:p-6">
        <h1 className="text-heading-300 text-text-primary">Error loading group</h1>
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
            title={label}
            titleSize="text-heading-300"
            inline
            icon={<ChevronDown className="rotate-90" />}
            iconAriaLabel="Back to groups"
            iconOnClick={() => navigate("/groups")}
          >
            <div className="ml-3 flex items-center gap-3">
              <Button variant={variants.secondary} onClick={() => navigate(`/miners?group=${group?.id}`)}>
                View miners
              </Button>
              <Button variant={variants.secondary} onClick={() => setShowEditModal(true)}>
                Edit group
              </Button>
              <DeviceSetActionsMenu
                memberDeviceIds={memberDeviceIds ?? []}
                onEdit={() => setShowEditModal(true)}
                onActionComplete={() => resolveGroup(label, group?.id)}
              />
            </div>
          </Header>
        </div>

        {/* Overview Section */}
        <section className="p-10 phone:p-6 tablet:p-6">
          <div className="flex flex-col gap-1">
            <FleetHealth
              title="Miners"
              fleetSize={
                streamingStateCounts || hasInitialLoadCompleted ? totalMiners : memberDeviceIds ? groupSize : undefined
              }
              healthyMiners={stateCounts?.hashingCount ?? (isEmptyGroup || hasInitialLoadCompleted ? 0 : undefined)}
              needsAttentionMiners={
                stateCounts?.brokenCount ?? (isEmptyGroup || hasInitialLoadCompleted ? 0 : undefined)
              }
              offlineMiners={stateCounts?.offlineCount ?? (isEmptyGroup || hasInitialLoadCompleted ? 0 : undefined)}
              sleepingMiners={stateCounts?.sleepingCount ?? (isEmptyGroup || hasInitialLoadCompleted ? 0 : undefined)}
              extraFilterParams={group ? `group=${group.id}` : undefined}
              totalMinersLink={group ? `/miners?group=${group.id}` : undefined}
            />
            <FleetErrors
              controlBoardErrors={controlBoardErrors}
              fanErrors={fanErrors}
              hashboardErrors={hashboardErrors}
              psuErrors={psuErrors}
              extraFilterParams={group ? `group=${group.id}` : undefined}
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
                  <span>Group</span>
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
            <DeviceSetPerformanceSection duration={duration} />
          </div>
          <div ref={refs.vertical.end} />
        </section>
      </div>

      {showEditModal && group && (
        <GroupModal
          show
          group={group}
          onDismiss={() => setShowEditModal(false)}
          onSuccess={() => {
            setShowEditModal(false);
            // Re-resolve group to pick up label and membership changes
            resolveGroup(label, group.id);
          }}
        />
      )}
    </div>
  );
};

export default GroupOverviewPage;
