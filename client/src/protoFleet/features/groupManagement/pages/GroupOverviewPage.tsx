import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { useDeviceSetStateCounts } from "@/protoFleet/api/useDeviceSetStateCounts";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import FleetHealth from "@/protoFleet/features/dashboard/components/FleetHealth";
import DeviceSetActionsMenu from "@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import GroupModal from "@/protoFleet/features/groupManagement/components/GroupModal";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { useDuration, useSetDuration } from "@/protoFleet/store";
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
      // eslint-disable-next-line react-hooks/set-state-in-effect -- flag not-found state when label missing
      setNotFound(true);
      setLoading(false);
      return;
    }

    setLoading(true);
    resolveGroup(label);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [label]);

  const duration = useDuration();
  const setDuration = useSetDuration();
  const { refs } = useStickyState();

  // Component errors scoped to group's devices
  // Pass undefined when no members yet (loading); pass empty array for truly empty groups
  // so useComponentErrors can distinguish "no scope" from "empty scope"
  const componentErrorsOptions = useMemo(
    () => (memberDeviceIds ? { deviceIdentifiers: memberDeviceIds, pollIntervalMs: POLL_INTERVAL_MS } : undefined),
    [memberDeviceIds],
  );
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors(componentErrorsOptions);

  // Group size for "X of Y miners reporting" subtitles
  const groupSize = memberDeviceIds?.length ?? 0;

  // Scoped state counts via getDeviceSetStats API
  const {
    totalMiners,
    stateCounts,
    hasLoaded: statsLoaded,
    refetch: refetchStats,
  } = useDeviceSetStateCounts({
    deviceSetId: group?.id,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  const isEmptyGroup = memberDeviceIds !== null && memberDeviceIds.length === 0;

  // Telemetry fetching - scoped to group's device IDs, polled
  const telemetryEnabled = memberDeviceIds !== null && memberDeviceIds.length > 0;

  const telemetryOptions = useMemo(
    () => ({
      deviceIds: memberDeviceIds ?? [],
      measurementTypes: ALL_MEASUREMENT_TYPES,
      aggregations: ALL_AGGREGATION_TYPES,
      duration,
      enabled: telemetryEnabled,
      pollIntervalMs: POLL_INTERVAL_MS,
    }),
    [memberDeviceIds, duration, telemetryEnabled],
  );

  const { data: telemetryData } = useTelemetryMetrics(telemetryOptions);

  // For empty groups, treat as "loaded with no data" so panels show "No data" not skeleton
  const metrics = isEmptyGroup ? [] : telemetryData?.metrics;

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (notFound) {
    return (
      <div className="p-6 laptop:p-10">
        <h1 className="text-heading-300 text-text-primary">Group not found</h1>
        <p className="mt-2 text-300 text-text-primary-50">No group with the label &ldquo;{label}&rdquo; exists.</p>
      </div>
    );
  }

  if (resolveError) {
    return (
      <div className="p-6 laptop:p-10">
        <h1 className="text-heading-300 text-text-primary">Error loading group</h1>
        <p className="mt-2 text-300 text-text-primary-50">{resolveError}</p>
      </div>
    );
  }

  return (
    <div className="h-full">
      <div className="flex flex-col">
        {/* Header */}
        <div className="p-6 pb-0 laptop:p-10 laptop:pb-0">
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
                deviceSetId={group?.id}
                onEdit={() => setShowEditModal(true)}
                onActionComplete={() => {
                  resolveGroup(label, group?.id);
                  void refetchStats();
                }}
              />
            </div>
          </Header>
        </div>

        {/* Overview Section */}
        <section className="p-6 laptop:p-10">
          <div className="flex flex-col gap-1">
            <FleetHealth
              title="Miners"
              fleetSize={stateCounts ? totalMiners : memberDeviceIds ? groupSize : undefined}
              healthyMiners={stateCounts?.hashingCount ?? (isEmptyGroup ? 0 : statsLoaded ? null : undefined)}
              needsAttentionMiners={stateCounts?.brokenCount ?? (isEmptyGroup ? 0 : statsLoaded ? null : undefined)}
              offlineMiners={stateCounts?.offlineCount ?? (isEmptyGroup ? 0 : statsLoaded ? null : undefined)}
              sleepingMiners={stateCounts?.sleepingCount ?? (isEmptyGroup ? 0 : statsLoaded ? null : undefined)}
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
          <div className="sticky top-0 z-2 bg-surface-5 px-6 pt-6 pb-6 laptop:px-10 laptop:pt-10 dark:bg-surface-base">
            <div className="flex flex-col gap-4 tablet:flex-row tablet:items-center tablet:justify-between">
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

          <div className="px-6 laptop:px-10">
            <DeviceSetPerformanceSection duration={duration} metrics={metrics} />
          </div>
          {/* eslint-disable-next-line react-hooks/refs -- ref object from useStickyState is passed to <div ref>; React writes .current during commit, not read during render */}
          <div ref={refs.vertical.end} />
        </section>
      </div>

      {showEditModal && group ? (
        <GroupModal
          show
          group={group}
          onDismiss={() => setShowEditModal(false)}
          onSuccess={() => {
            setShowEditModal(false);
            resolveGroup(label, group.id);
            void refetchStats();
          }}
        />
      ) : null}
    </div>
  );
};

export default GroupOverviewPage;
