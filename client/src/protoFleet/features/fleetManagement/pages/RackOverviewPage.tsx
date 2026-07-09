import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";

import { create } from "@bufbuild/protobuf";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import {
  type DeviceSet,
  type RackCoolingType,
  type RackOrderIndex,
  RackSlotPositionSchema,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { useDeviceSetStateCounts } from "@/protoFleet/api/useDeviceSetStateCounts";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { ManageRackModal, type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal";
import ReparentWarningDialog from "@/protoFleet/features/fleetManagement/components/ManageRackModal/ReparentWarningDialog";
import SearchMinersModal from "@/protoFleet/features/fleetManagement/components/ManageRackModal/SearchMinersModal";
import { orderIndexToOrigin } from "@/protoFleet/features/fleetManagement/components/ManageRackModal/types";
import type { SlotHealthState } from "@/protoFleet/features/fleetManagement/components/RackDetailGrid/types";
import { RackHealthModule } from "@/protoFleet/features/fleetManagement/components/RackHealthModule";
import { SLOT_STATUS_MAP } from "@/protoFleet/features/fleetManagement/utils/rackCardMapper";
import DeviceSetActionsMenu from "@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import Breadcrumb, { type BreadcrumbSegment, type BreadcrumbSibling } from "@/shared/components/Breadcrumb";
import Button, { sizes, variants } from "@/shared/components/Button";
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
];

const ALL_AGGREGATION_TYPES: AggregationType[] = [AggregationType.AVERAGE, AggregationType.MIN, AggregationType.MAX];

const RackOverviewPage = () => {
  const { rackId: rackIdParam } = useParams<{ rackId: string }>();
  const navigate = useNavigate();
  const activeSite = useFleetStore((state) => state.ui.activeSite);

  // Rack resolution state
  const [rack, setRack] = useState<DeviceSet | null>(null);
  const [memberDeviceIds, setMemberDeviceIds] = useState<string[] | null>(null);
  const [allBuildings, setAllBuildings] = useState<BuildingWithCounts[]>([]);
  const [rackSiblingState, setRackSiblingState] = useState<{ key: string; siblings: BreadcrumbSibling[] } | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [showEditModal, setShowEditModal] = useState(false);
  const [searchMinerSlot, setSearchMinerSlot] = useState<{ row: number; col: number } | null>(null);
  // Pending reparent confirmation for the quick slot-assign flow (#672).
  const [reparentPrompt, setReparentPrompt] = useState<{ count: number; onConfirm: () => void } | null>(null);
  const sleepActionRef = useRef<(() => void) | null>(null);
  const actionActiveRef = useRef(false);

  const { getDeviceSet, listGroupMembers, assignDevicesToRack, setRackSlotPosition, deleteGroup, listRacks } =
    useDeviceSets();
  const { listAllBuildings } = useBuildings();
  // Site catalog comes from the shared shell-level provider (used here only to
  // label the rack's parent site in breadcrumbs), so this page no longer fires
  // its own ListSites.
  const { sites } = useSitesContext();

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

  useEffect(() => {
    const controller = new AbortController();
    void listAllBuildings({
      signal: controller.signal,
      onSuccess: setAllBuildings,
      onError: () => setAllBuildings([]),
    });
    return () => controller.abort();
  }, [listAllBuildings]);

  // Initial resolution from URL param
  useEffect(() => {
    if (!rackIdParam) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- flag not-found state when URL param missing
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
  const siteNameById = useMemo(
    () =>
      new Map(
        (sites ?? []).filter((row) => row.site !== undefined).map((row) => [row.site!.id.toString(), row.site!.name]),
      ),
    [sites],
  );
  const buildingById = useMemo(
    () =>
      new Map(
        allBuildings
          .filter((row) => row.building !== undefined)
          .map((row) => [row.building!.id.toString(), row.building!]),
      ),
    [allBuildings],
  );
  const rackBuildingId = rackInfo?.buildingId?.toString();
  const rackBuilding = rackBuildingId ? buildingById.get(rackBuildingId) : undefined;
  const rackSiteId = rackInfo?.siteId?.toString() ?? rackBuilding?.siteId?.toString();
  const rackSiblingKey = rack?.id.toString() ?? "";
  const currentRackSiblings = rackSiblingState?.key === rackSiblingKey ? rackSiblingState.siblings : [];

  useEffect(() => {
    if (!rack || !rackInfo) return;

    let cancelled = false;
    const currentSiblingKey = rackSiblingKey;
    const applySiblings = (siblings: BreadcrumbSibling[]) => {
      if (!cancelled) setRackSiblingState({ key: currentSiblingKey, siblings });
    };
    const currentRackId = rack.id;
    const siblingScope =
      rackInfo.buildingId !== undefined
        ? { buildingIds: [rackInfo.buildingId] }
        : rackSiteId
          ? { siteIds: [BigInt(rackSiteId)] }
          : { includeUnassigned: true };
    void listRacks({
      ...siblingScope,
      onSuccess: (racks) =>
        applySiblings(
          racks
            .filter((candidate) => candidate.typeDetails.case === "rackInfo")
            .map((candidate) => ({
              label: candidate.label || "Rack",
              to: `/racks/${candidate.id.toString()}`,
              isActive: candidate.id === currentRackId,
            })),
        ),
      onError: () => applySiblings([]),
    });

    return () => {
      cancelled = true;
    };
  }, [listRacks, rack, rackInfo, rackSiblingKey, rackSiteId]);

  const duration = useDuration();
  const setDuration = useSetDuration();
  const { refs } = useStickyState();
  const { bgClass } = usePageBackground();

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

  // ManageRackModal form data (for edit rack flow)
  const assignMinersFormData = useMemo<RackFormData | null>(() => {
    if (!showEditModal || !rack || !rackInfo) return null;
    return {
      label: rack.label,
      zone: rackInfo.zone ?? "",
      rows: rackInfo.rows ?? 1,
      columns: rackInfo.columns ?? 1,
      orderIndex: rackInfo.orderIndex as RackOrderIndex,
      coolingType: rackInfo.coolingType as RackCoolingType,
      // Seed current placement (0 → undefined) so the settings dropdowns and
      // miner eligibility reflect where the rack lives.
      siteId: rack.placement?.site?.id || undefined,
      buildingId: rack.placement?.building?.id || undefined,
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
      <div className="p-6 laptop:p-10">
        <h1 className="text-heading-300 text-text-primary">Rack not found</h1>
        <p className="mt-2 text-300 text-text-primary-50">No rack with ID &ldquo;{rackIdParam}&rdquo; exists.</p>
      </div>
    );
  }

  if (resolveError) {
    return (
      <div className="p-6 laptop:p-10">
        <h1 className="text-heading-300 text-text-primary">Error loading rack</h1>
        <p className="mt-2 text-300 text-text-primary-50">{resolveError}</p>
      </div>
    );
  }

  const rackBreadcrumbSegments: BreadcrumbSegment[] = [];
  if (rackSiteId) {
    rackBreadcrumbSegments.push({ label: "Sites", to: "/fleet/sites" });
    rackBreadcrumbSegments.push({ label: siteNameById.get(rackSiteId) ?? "Site", to: `/sites/${rackSiteId}` });
    if (rackBuildingId) {
      rackBreadcrumbSegments.push({
        label: rackBuilding?.name || "Building",
        to: `/buildings/${rackBuildingId}`,
      });
    }
  } else {
    rackBreadcrumbSegments.push({
      label: "Racks",
      to: scopedPath("/fleet/racks", activeSite),
    });
  }
  rackBreadcrumbSegments.push({
    label: rack?.label ?? "Rack",
    siblings: currentRackSiblings.length > 1 ? currentRackSiblings : undefined,
  });

  return (
    <div className="h-full">
      <div className="flex flex-col">
        {/* Header */}
        <div className="p-6 pb-0 laptop:p-10 laptop:pb-0">
          <div className="flex flex-col gap-3">
            <Breadcrumb segments={rackBreadcrumbSegments} testId="rack-page-breadcrumb" />
            <Header
              title={rack?.label ?? ""}
              titleSize="text-heading-300"
              subtitle={rackInfo?.zone || undefined}
              subtitleSize="text-300"
              subtitleClassName="text-text-primary"
              inline
            >
              <div className="ml-3 flex items-center gap-3">
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => navigate(scopedPath(`/fleet/miners?rack=${rack?.id}`, activeSite))}
                >
                  View miners
                </Button>
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => sleepActionRef.current?.()}
                  disabled={!memberDeviceIds || memberDeviceIds.length === 0}
                >
                  Sleep all miners
                </Button>
                <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowEditModal(true)}>
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
        </div>

        {/* Health Overview Section */}
        <section className="px-6 pt-6 pb-6 laptop:px-10 laptop:pt-6 laptop:pb-10">
          <div className="flex flex-col gap-4">
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
              activeSite={activeSite}
            />
            <FleetErrors
              controlBoardErrors={controlBoardErrors}
              fanErrors={fanErrors}
              hashboardErrors={hashboardErrors}
              psuErrors={psuErrors}
              extraFilterParams={rack ? `rack=${rack.id}` : undefined}
              activeSite={activeSite}
            />
          </div>
        </section>

        {/* Performance Section */}
        <section className="pb-6">
          <div ref={refs.vertical.start} />
          <div className={`${bgClass} sticky top-0 z-2 px-6 pt-6 pb-6 laptop:px-10 laptop:pt-10`}>
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

          <div className="px-6 pt-4 laptop:px-10">
            <DeviceSetPerformanceSection duration={duration} metrics={metrics} />
          </div>
          {/* eslint-disable-next-line react-hooks/refs -- ref object from useStickyState is passed to <div ref>; React writes .current during commit, not read during render */}
          <div ref={refs.vertical.end} />
        </section>
      </div>

      {showEditModal && rack && assignMinersFormData ? (
        <ManageRackModal
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
          // Settings "Continue" persists before the final Save; refresh the
          // overview in the background (modal stays open) so a later dismiss
          // can't leave stale label/placement on screen.
          onSettingsPersisted={() => {
            resolveRack(rack.id);
            void refetchStats();
          }}
          onDelete={() =>
            new Promise<void>((resolve, reject) => {
              deleteGroup({
                deviceSetId: rack.id,
                onSuccess: () => {
                  pushToast({ message: "Rack deleted", status: STATUSES.success });
                  navigate(scopedPath("/fleet/racks", activeSite));
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
      ) : null}

      {searchMinerSlot && rack ? (
        <SearchMinersModal
          show
          eligibility={{
            rackId: rack.id,
            siteId: rack.placement?.site?.id || undefined,
            buildingId: rack.placement?.building?.id || undefined,
          }}
          targetRackLabel={rack.label}
          onDismiss={() => setSearchMinerSlot(null)}
          onConfirm={(minerId, isReassignment) => {
            const slot = searchMinerSlot;
            setSearchMinerSlot(null);

            // Two-step: atomically move the miner into this rack
            // (clearing any prior rack membership in one tx), then
            // assign the slot. No single API supports both atomically
            // without resending the full rack state. On partial
            // success (assigned but slot failed), we still refresh so
            // the UI stays consistent.
            // `force` clears a conflicting site when the target rack has no
            // site of its own; without it the server returns conflicts and
            // writes nothing. We force only after the operator has confirmed the
            // reparent below.
            const assign = (force: boolean) =>
              assignDevicesToRack({
                targetRackId: rack.id,
                deviceIdentifiers: [minerId],
                forceClearConflictingSite: force,
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
                // Defensive: a placement conflict without a preceding warning
                // shouldn't be a silent no-op.
                onConflicts: () => {
                  pushToast({
                    message: "This miner is assigned to another site. Confirm the move and try again.",
                    status: STATUSES.error,
                  });
                },
                onError: (msg) => {
                  pushToast({ message: msg, status: STATUSES.error });
                },
              });

            // Reparenting a miner from another rack/building/site warns first (#672);
            // confirming forces the site strip so a site-less target rack still writes.
            if (isReassignment) {
              setReparentPrompt({ count: 1, onConfirm: () => assign(true) });
            } else {
              assign(false);
            }
          }}
        />
      ) : null}

      {reparentPrompt && rack ? (
        <ReparentWarningDialog
          count={reparentPrompt.count}
          rackLabel={rack.label}
          onCancel={() => setReparentPrompt(null)}
          onConfirm={() => {
            const proceed = reparentPrompt.onConfirm;
            setReparentPrompt(null);
            proceed();
          }}
        />
      ) : null}
    </div>
  );
};

export default RackOverviewPage;
