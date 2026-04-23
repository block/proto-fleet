import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";

import { create } from "@bufbuild/protobuf";

import {
  type DeviceSet,
  type RackCoolingType,
  type RackOrderIndex,
  RackSlotPositionSchema,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { useDeviceSetStateCounts } from "@/protoFleet/api/useDeviceSetStateCounts";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import DeviceSetActionsMenu from "@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import {
  AssignMinersModal,
  type RackFormData,
} from "@/protoFleet/features/rackManagement/components/AssignMinersModal";
import SearchMinersModal from "@/protoFleet/features/rackManagement/components/AssignMinersModal/SearchMinersModal";
import { orderIndexToOrigin } from "@/protoFleet/features/rackManagement/components/AssignMinersModal/types";
import type { SlotHealthState } from "@/protoFleet/features/rackManagement/components/RackDetailGrid/types";
import { RackHealthModule } from "@/protoFleet/features/rackManagement/components/RackHealthModule";
import { SLOT_STATUS_MAP } from "@/protoFleet/features/rackManagement/utils/rackCardMapper";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";
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

const RackOverviewPage = () => {
  const { rackId: rackIdParam } = useParams<{ rackId: string }>();
  const navigate = useNavigate();

  // Rack resolution state
  const [rack, setRack] = useState<DeviceSet | null>(null);
  const [memberDeviceIds, setMemberDeviceIds] = useState<string[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [showEditModal, setShowEditModal] = useState(false);
  const [searchMinerSlot, setSearchMinerSlot] = useState<{ row: number; col: number } | null>(null);
  const sleepActionRef = useRef<(() => void) | null>(null);
  const actionActiveRef = useRef(false);

  const { getDeviceSet, listGroupMembers, addDevicesToDeviceSet, setRackSlotPosition, deleteGroup } = useDeviceSets();

  // Request versioning to guard against stale resolution callbacks
  const resolveVersionRef = useRef(0);

  // Resolve rack by ID → set rack + member device IDs
  // When `silent` is true (polling), keep existing state visible while refreshing in the background.
  const resolveRack = useCallback(
    (rackId: bigint, { silent = false } = {}) => {
      const version = ++resolveVersionRef.current;
      if (!silent) {
        setLoading(true);
        setRack(null);
        setMemberDeviceIds(null);
        setNotFound(false);
        setResolveError(null);
      }

      getDeviceSet({
        deviceSetId: rackId,
        onSuccess: (deviceSet) => {
          if (version !== resolveVersionRef.current) return;

          // Reject non-rack device sets
          if (deviceSet.typeDetails.case !== "rackInfo") {
            setNotFound(true);
            setLoading(false);
            return;
          }

          setRack(deviceSet);
          // Clear any latched error state from a prior failed poll
          setNotFound(false);
          setResolveError(null);

          // Fetch member device IDs
          listGroupMembers({
            deviceSetId: deviceSet.id,
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
              setLoading(false);
            },
            onError: (msg) => {
              if (version !== resolveVersionRef.current) return;
              if (!silent) {
                setResolveError(msg);
              }
              setLoading(false);
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
    [getDeviceSet, listGroupMembers],
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
    }, POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [loading, rack, showEditModal, resolveRack]);

  // Rack metadata
  const rackInfo = rack?.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
  const rows = rackInfo?.rows ?? 1;
  const cols = rackInfo?.columns ?? 1;
  const orderIndex = rackInfo?.orderIndex;
  const numberingOrigin = orderIndex !== undefined ? orderIndexToOrigin(orderIndex) : "bottom-left";

  const duration = useDuration();
  const setDuration = useSetDuration();
  const { refs } = useStickyState();

  // Component errors scoped to rack's devices
  const componentErrorsOptions = useMemo(
    () => (memberDeviceIds ? { deviceIdentifiers: memberDeviceIds, pollIntervalMs: POLL_INTERVAL_MS } : undefined),
    [memberDeviceIds],
  );
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors(componentErrorsOptions);

  // Scoped state counts + slot grid data via getDeviceSetStats API
  const {
    stateCounts,
    stats: deviceSetStats,
    hasLoaded: statsLoaded,
    refetch: refetchStats,
  } = useDeviceSetStateCounts({
    deviceSetId: rack?.id,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  // Build slot states for RackDetailGrid from device set stats
  const slotStates = useMemo<Record<string, SlotHealthState>>(() => {
    if (!deviceSetStats) return {};
    const states: Record<string, SlotHealthState> = {};
    for (const s of deviceSetStats.slotStatuses) {
      states[`${s.row}-${s.column}`] = SLOT_STATUS_MAP[s.status] ?? "empty";
    }
    return states;
  }, [deviceSetStats]);

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

  const isEmptyRack = memberDeviceIds !== null && memberDeviceIds.length === 0;

  // Telemetry fetching - scoped to rack's device IDs, polled
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

  // For empty racks, treat as "loaded with no data" so panels show "No data" not skeleton
  const metrics = isEmptyRack ? [] : telemetryData?.metrics;

  if (loading || (rack && !statsLoaded)) {
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
            subtitle={rackInfo?.zone || undefined}
            subtitleSize="text-300"
            subtitleClassName="text-text-primary"
            inline
            icon={<ChevronDown className="rotate-90" />}
            iconAriaLabel="Back to racks"
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
              <DeviceSetActionsMenu
                memberDeviceIds={memberDeviceIds ?? []}
                deviceSetId={rack?.id}
                deviceSetType="rack"
                onEdit={() => setShowEditModal(true)}
                editLabel="Edit rack"
                onActionComplete={() => {
                  if (rack) {
                    resolveRack(rack.id);
                    void refetchStats();
                  }
                }}
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
              onEmptySlotClick={(row, col) => setSearchMinerSlot({ row, col })}
              hashingCount={stateCounts?.hashingCount ?? (isEmptyRack ? 0 : statsLoaded ? null : undefined)}
              needsAttentionCount={stateCounts?.brokenCount ?? (isEmptyRack ? 0 : statsLoaded ? null : undefined)}
              offlineCount={stateCounts?.offlineCount ?? (isEmptyRack ? 0 : statsLoaded ? null : undefined)}
              sleepingCount={stateCounts?.sleepingCount ?? (isEmptyRack ? 0 : statsLoaded ? null : undefined)}
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
            <DeviceSetPerformanceSection duration={duration} metrics={metrics} />
          </div>
          {/* eslint-disable-next-line react-hooks/refs -- ref object from useStickyState is passed to <div ref>; React writes .current during commit, not read during render */}
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
            void refetchStats();
          }}
          onDelete={() =>
            new Promise<void>((resolve, reject) => {
              deleteGroup({
                deviceSetId: rack.id,
                onSuccess: () => {
                  pushToast({ message: "Rack deleted", status: STATUSES.success });
                  navigate("/racks");
                  resolve();
                },
                onError: (msg) => {
                  pushToast({ message: msg, status: STATUSES.error });
                  reject(new Error(msg));
                },
              });
            })
          }
        />
      )}

      {searchMinerSlot && rack && (
        <SearchMinersModal
          show
          currentRackLabel={rack.label}
          onDismiss={() => setSearchMinerSlot(null)}
          onConfirm={(minerId) => {
            const slot = searchMinerSlot;
            setSearchMinerSlot(null);

            // Two-step: add miner to rack membership, then assign to slot.
            // No single API supports both atomically without resending the full rack state.
            // On partial success (added but slot failed), we still refresh so the UI stays consistent.
            addDevicesToDeviceSet({
              deviceSetId: rack.id,
              deviceIdentifiers: [minerId],
              onSuccess: () => {
                setRackSlotPosition({
                  deviceSetId: rack.id,
                  deviceIdentifier: minerId,
                  position: create(RackSlotPositionSchema, { row: slot.row, column: slot.col }),
                  onSuccess: () => {
                    pushToast({ message: "Miner assigned to slot", status: STATUSES.success });
                    resolveRack(rack.id);
                    void refetchStats();
                  },
                  onError: (msg) => {
                    pushToast({
                      message: `Miner added to rack but slot assignment failed: ${msg}`,
                      status: STATUSES.error,
                    });
                    resolveRack(rack.id);
                    void refetchStats();
                  },
                });
              },
              onError: (msg) => {
                pushToast({ message: msg, status: STATUSES.error });
              },
            });
          }}
        />
      )}
    </div>
  );
};

export default RackOverviewPage;
