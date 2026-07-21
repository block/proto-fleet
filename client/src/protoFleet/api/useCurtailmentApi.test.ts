import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { type Timestamp, TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  applyActiveCurtailmentEvent,
  refreshActiveCurtailmentData,
  resetActiveCurtailmentData,
} from "@/protoFleet/api/activeCurtailmentData";
import { CURTAILMENT_CHANGED_EVENT } from "@/protoFleet/api/curtailmentEvents";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentMode,
  CurtailmentPriority,
  CurtailmentScopeSchema,
  CurtailmentTargetRollupSchema,
  CurtailmentTargetSchema,
  CurtailmentTargetState,
  CurtailmentUnavailableReasonSchema,
  FixedKwParamsSchema,
  FullFleetParamsSchema,
  ScopeDeviceListSchema,
  ScopeSiteSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

const {
  mockAdminTerminateEvent,
  mockForceReleaseCurtailmentOwnership,
  mockListActiveCurtailments,
  mockGetCurtailmentEvent,
  mockHandleAuthErrors,
  mockListCurtailmentEvents,
  mockStartCurtailment,
  mockStopCurtailment,
  mockUpdateCurtailment,
} = vi.hoisted(() => ({
  mockAdminTerminateEvent: vi.fn(),
  mockForceReleaseCurtailmentOwnership: vi.fn(),
  mockListActiveCurtailments: vi.fn(),
  mockGetCurtailmentEvent: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListCurtailmentEvents: vi.fn(),
  mockStartCurtailment: vi.fn(),
  mockStopCurtailment: vi.fn(),
  mockUpdateCurtailment: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: (() => {
    let activeEvents: CurtailmentEvent[] = [];

    return {
      listActiveCurtailments: async (...args: unknown[]) => {
        const response = (await mockListActiveCurtailments(...args)) as {
          event?: CurtailmentEvent;
          events?: CurtailmentEvent[];
        };
        activeEvents = response.events ?? (response.event ? [response.event] : []);
        return { events: activeEvents };
      },
      getCurtailmentEvent: async (request: { eventUuid: string }, ...args: unknown[]) =>
        (await mockGetCurtailmentEvent(request, ...args)) ?? {
          event: activeEvents.find((event) => event.eventUuid === request.eventUuid),
        },
      listCurtailmentEvents: mockListCurtailmentEvents,
      startCurtailment: mockStartCurtailment,
      stopCurtailment: mockStopCurtailment,
      adminTerminateEvent: mockAdminTerminateEvent,
      forceReleaseCurtailmentOwnership: mockForceReleaseCurtailmentOwnership,
      updateCurtailmentEvent: mockUpdateCurtailment,
    };
  })(),
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

const baseSubmitValues: CurtailmentSubmitValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  deviceSetIds: [],
  deviceIdentifiers: [],
  responseProfileId: "customPlan",
  curtailmentMode: "fixedKwReduction",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "5",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "",
  maxDurationSec: "",
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
  restoreBatchSize: "10",
  restoreIntervalSec: "60",
  reason: "Grid peak",
  includeMaintenance: false,
  forceIncludeAllPairedMiners: false,
};

function timestamp(isoDate: string): Timestamp {
  const date = new Date(isoDate);
  const milliseconds = date.getTime();

  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(milliseconds / 1000)),
    nanos: (milliseconds % 1000) * 1_000_000,
  });
}

function curtailmentEvent(overrides: Partial<CurtailmentEvent> = {}): CurtailmentEvent {
  const event = create(CurtailmentEventSchema, {
    eventUuid: "curt-1",
    reason: "Grid peak",
    state: CurtailmentEventState.ACTIVE,
    mode: CurtailmentMode.FIXED_KW,
    priority: CurtailmentPriority.EMERGENCY,
    scope: {
      case: "wholeOrg",
      value: create(ScopeWholeOrgSchema, {}),
    },
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 5 }),
    },
    effectiveBatchSize: 10,
    restoreBatchIntervalSec: 60,
    targetRollup: create(CurtailmentTargetRollupSchema, {
      confirmed: 1,
      dispatched: 1,
      total: 2,
    }),
    targets: [
      create(CurtailmentTargetSchema, {
        state: CurtailmentTargetState.CONFIRMED,
        baselinePowerW: 3000,
        observedPowerW: 500,
      }),
      create(CurtailmentTargetSchema, {
        state: CurtailmentTargetState.DISPATCHED,
        baselinePowerW: 3000,
        observedPowerW: 500,
      }),
    ],
    decisionSnapshot: {
      estimated_reduction_kw: 6.2,
      selected_count: 2,
    },
    startedAt: timestamp("2026-05-01T12:00:00Z"),
    createdAt: timestamp("2026-05-01T11:58:00Z"),
  });

  return Object.assign(event, overrides);
}

describe("useCurtailmentApi", () => {
  beforeEach(() => {
    resetActiveCurtailmentData();
    vi.clearAllMocks();
    mockGetCurtailmentEvent.mockReset();
    mockHandleAuthErrors.mockImplementation(({ onError }: { error: unknown; onError?: (error: unknown) => void }) =>
      onError?.(new Error("auth error")),
    );
    mockListActiveCurtailments.mockResolvedValue({ event: undefined });
    mockListCurtailmentEvents.mockResolvedValue({ events: [], nextPageToken: "" });
  });

  it("loads and maps active curtailment plus history", async () => {
    const activeEvent = curtailmentEvent({
      facilityFanDeviceIds: [31n, 32n],
      fanOffDelaySec: 0,
      fanRestoreDelaySec: 0,
      fanOffSentAt: timestamp("2026-05-01T12:05:00Z"),
      fanAirflowReopenedAt: timestamp("2026-05-01T12:10:00Z"),
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-2",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-1");
    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        reason: "Grid peak",
        state: "active",
        scopeLabel: "Whole fleet",
        selectedMiners: 2,
        estimatedReductionKw: 6.2,
        targetKw: 5,
        facilityFanDeviceCount: 2,
        fanOffSentAt: "2026-05-01T12:05:00.000Z",
        fanAirflowReopenedAt: "2026-05-01T12:10:00.000Z",
        observedReductionKw: 5,
        remainingPowerKw: 1,
        // Live-progress inputs (issue #660): elapsed anchors on startedAt,
        // falling back to createdAt for open-loop events that stamp
        // started_at only after confirmation.
        startedAt: "2026-05-01T12:00:00.000Z",
        createdAt: "2026-05-01T11:58:00.000Z",
      }),
    );
    expect(result.current.activeEventFormValues).toEqual(
      expect.objectContaining({
        reason: "Grid peak",
        scopeType: "wholeOrg",
        targetKw: "5",
        priority: "emergency",
        curtailBatchSize: "",
        curtailBatchIntervalSec: "",
        restoreBatchSize: "",
        restoreIntervalSec: "60",
        facilityFanDeviceIds: ["31", "32"],
        fanOffDelaySec: "0",
        fanRestoreDelaySec: "0",
      }),
    );
    expect(result.current.activeEvent?.rollups).toEqual([
      { state: "dispatched", count: 1 },
      { state: "confirmed", count: 1 },
    ]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-1", "curt-2"]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        priority: "emergency",
        sourceLabel: "Manual",
        facilityFanDeviceCount: 2,
        startedAt: "2026-05-01T12:00:00.000Z",
      }),
    );
  });

  it("maps unavailable target reasons for active curtailment progress", async () => {
    const activeEvent = curtailmentEvent({
      targetRollup: create(CurtailmentTargetRollupSchema, {
        unavailable: 5,
        total: 5,
        unavailableReasons: [
          create(CurtailmentUnavailableReasonSchema, { reason: "offline", count: 3 }),
          create(CurtailmentUnavailableReasonSchema, { reason: "authentication_needed", count: 1 }),
          create(CurtailmentUnavailableReasonSchema, { reason: "maintenance", count: 1 }),
        ],
      }),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.unavailableReasonCounts).toEqual([
      { label: "offline", count: 3 },
      { label: "in maintenance", count: 1 },
      { label: "needs authentication", count: 1 },
    ]);
  });

  it("falls back to humanized raw codes for unmapped or blank unavailable reasons", async () => {
    // Servers can grow new canonical reason codes ahead of the client's label
    // map; the raw code (underscores humanized) must render rather than break.
    const activeEvent = curtailmentEvent({
      targetRollup: create(CurtailmentTargetRollupSchema, {
        unavailable: 5,
        total: 5,
        unavailableReasons: [
          create(CurtailmentUnavailableReasonSchema, { reason: "some_new_code", count: 3 }),
          create(CurtailmentUnavailableReasonSchema, { reason: "   ", count: 2 }),
        ],
      }),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.unavailableReasonCounts).toEqual([
      { label: "some new code", count: 3 },
      { label: "reason unknown", count: 2 },
    ]);
  });

  it("prefers the live rollup total over the snapshot count for active events", async () => {
    // Closed-loop claims / all-paired policy changes can grow the live target
    // set far past the event-start snapshot; active surfaces show the live
    // rollup as the operational truth.
    const activeEvent = curtailmentEvent({
      decisionSnapshot: {
        estimated_reduction_kw: 6.2,
        selected_count: 10,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 4000,
        pending: 1000,
        total: 5000,
      }),
      targets: [],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.selectedMiners).toBe(5000);
    // The estimate-derived observed reduction scales by the live total
    // (6.2 * 4000/5000), not the stale snapshot count (6.2 * 4000/10).
    expect(result.current.activeEvent?.observedReductionKw).toBeCloseTo(4.96);
    // The injected active history row is a live surface too.
    expect(result.current.activeEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-1",
        selectedMiners: 5000,
        displayState: "curtailing",
      }),
    );
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-1",
        selectedMiners: 5000,
      }),
    );
  });

  it("keeps live counts while marking summary-only active rows as missing a kW estimate", async () => {
    // Non-selected active-list rows carry a live rollup but a scrubbed
    // decision snapshot and no targets; the injected history row must not
    // fabricate a 0.0 kW estimate from that shape.
    const selectedEvent = curtailmentEvent();
    const summaryOnlyEvent = curtailmentEvent({
      eventUuid: "curt-summary-only",
      decisionSnapshot: undefined,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 4000,
        pending: 1000,
        total: 5000,
      }),
      targets: [],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [selectedEvent, summaryOnlyEvent] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: selectedEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvents[1]).toEqual(
      expect.objectContaining({
        id: "curt-summary-only",
        selectedMiners: 5000,
        targetMetricsAvailable: true,
        estimatedReductionAvailable: false,
        estimatedReductionKw: 0,
      }),
    );
  });

  it("backfills the kW estimate for summary-only active rows from matching history", async () => {
    const selectedEvent = curtailmentEvent();
    const summaryOnlyEvent = curtailmentEvent({
      eventUuid: "curt-summary-backfill",
      decisionSnapshot: undefined,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 4000,
        pending: 1000,
        total: 5000,
      }),
      targets: [],
    });
    const historyEvent = curtailmentEvent({
      eventUuid: "curt-summary-backfill",
      decisionSnapshot: {
        estimated_reduction_kw: 8.4,
        selected_count: 10,
      },
      targetRollup: undefined,
      targets: [],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [selectedEvent, summaryOnlyEvent] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: selectedEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [historyEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvents[1]).toEqual(
      expect.objectContaining({
        id: "curt-summary-backfill",
        selectedMiners: 5000,
        estimatedReductionKw: 8.4,
        estimatedReductionAvailable: true,
      }),
    );
  });

  it("keeps the snapshot count as audit context for completed history events", async () => {
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-history-audit",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      decisionSnapshot: {
        estimated_reduction_kw: 6.2,
        selected_count: 10,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 5000,
        total: 5000,
      }),
      targets: [],
    });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-history-audit",
        selectedMiners: 10,
      }),
    );
  });

  it("updates active target counts from active polling", async () => {
    const initialEvent = curtailmentEvent({
      decisionSnapshot: {
        estimated_reduction_kw: 6.2,
        selected_count: 10,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 10,
        total: 10,
      }),
      targets: [],
    });
    const grownEvent = curtailmentEvent({
      decisionSnapshot: {
        estimated_reduction_kw: 6.2,
        selected_count: 10,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 4000,
        pending: 1000,
        total: 5000,
      }),
      targets: [],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: initialEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });
    expect(result.current.activeEvent?.selectedMiners).toBe(10);

    mockListActiveCurtailments.mockResolvedValueOnce({ event: grownEvent });
    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.selectedMiners).toBe(5000);
  });

  it("maps MQTT automation ownership onto active and history events", async () => {
    const activeEvent = curtailmentEvent({
      externalSource: "curtailment_automation",
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        isAutomationOwned: true,
        sourceLabel: "Curtailment automation",
      }),
    );
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        sourceLabel: "Curtailment automation",
      }),
    );
  });

  it("clears cached active state when refresh loses curtailment read permission", async () => {
    const activeEvent = curtailmentEvent({ eventUuid: "curt-permission-loss" });
    applyActiveCurtailmentEvent(activeEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockRejectedValueOnce(new ConnectError("permission denied", Code.PermissionDenied));
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    expect(result.current.activeEventId).toBe("curt-permission-loss");

    await act(async () => {
      await result.current.refreshCurtailment().catch(() => undefined);
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
    expect(result.current.activeEvents).toEqual([]);
    expect(mockHandleAuthErrors).toHaveBeenCalledWith({
      error: expect.any(ConnectError),
    });
  });

  it("lists multiple active curtailments while hydrating only the selected detail", async () => {
    const firstActiveEvent = curtailmentEvent({
      eventUuid: "curt-site-a",
      reason: "Site A event",
    });
    const secondActiveSummary = curtailmentEvent({
      eventUuid: "curt-site-b",
      reason: "Site B event",
      decisionSnapshot: undefined,
      targetRollup: undefined,
      targets: [],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [firstActiveEvent, secondActiveSummary] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: firstActiveEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-site-a");
    expect(result.current.activeEvents.map((event) => event.id)).toEqual(["curt-site-a", "curt-site-b"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-site-a", "curt-site-b"]);
    expect(result.current.activeEvents[0]).toEqual(
      expect.objectContaining({
        selectedMiners: 2,
        estimatedReductionKw: 6.2,
        targetMetricsAvailable: true,
      }),
    );
    expect(result.current.activeEvents[1]).toEqual(
      expect.objectContaining({
        id: "curt-site-b",
        targetMetricsAvailable: false,
      }),
    );
    expect(mockGetCurtailmentEvent).toHaveBeenCalledOnce();
    expect(mockGetCurtailmentEvent).toHaveBeenCalledWith(
      expect.objectContaining({ eventUuid: "curt-site-a" }),
      expect.anything(),
    );
  });

  it("maps full-fleet active events to full-fleet form values", async () => {
    const activeEvent = curtailmentEvent({
      mode: CurtailmentMode.FULL_FLEET,
      modeParams: { case: undefined },
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventFormValues).toEqual(
      expect.objectContaining({
        curtailmentMode: "fullFleet",
        targetKw: "",
        toleranceKw: "",
      }),
    );
    expect(result.current.activeEvent?.targetKw).toBeUndefined();
  });

  it("uses loaded site names for site-scoped active and history events", async () => {
    const siteScopedEvent = curtailmentEvent({
      scope: {
        case: "site",
        value: create(ScopeSiteSchema, { siteId: 101n }),
      },
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: siteScopedEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [siteScopedEvent], nextPageToken: "" });
    const siteNameById = new Map([["101", "Austin, TX"]]);

    const { result } = renderHook(() => useCurtailmentApi({ siteNameById }));

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        scopeLabel: "Austin, TX",
      }),
    );
    expect(result.current.activeEventFormValues).toEqual(
      expect.objectContaining({
        scopeType: "site",
        scopeId: "Austin, TX",
        siteId: "101",
      }),
    );
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        scopeLabel: "Austin, TX",
      }),
    );
  });

  it("uses combined scope labels for mixed site and miner active and history events", async () => {
    const mixedScopedEvent = curtailmentEvent({
      scopes: [
        create(CurtailmentScopeSchema, {
          scope: { case: "site", value: create(ScopeSiteSchema, { siteId: 101n }) },
        }),
        create(CurtailmentScopeSchema, {
          scope: {
            case: "deviceIdentifiers",
            value: create(ScopeDeviceListSchema, { deviceIdentifiers: ["miner-1"] }),
          },
        }),
      ],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: mixedScopedEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [mixedScopedEvent], nextPageToken: "" });
    const siteNameById = new Map([["101", "Calgary"]]);

    const { result } = renderHook(() => useCurtailmentApi({ siteNameById }));

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        scopeLabel: "Calgary + 1 miner",
      }),
    );
    expect(result.current.activeEventFormValues).toEqual(
      expect.objectContaining({
        scopeType: "explicitMiners",
        scopeId: "Calgary",
        siteSelection: "site",
        siteId: "101",
        deviceIdentifiers: ["miner-1"],
      }),
    );
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        scopeLabel: "Calgary + 1 miner",
      }),
    );
  });

  it("falls back to Site id labels for site-scoped events without a loaded site name", async () => {
    const siteScopedEvent = curtailmentEvent({
      scope: {
        case: "site",
        value: create(ScopeSiteSchema, { siteId: 101n }),
      },
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: siteScopedEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [siteScopedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.scopeLabel).toBe("Site 101");
    expect(result.current.activeEventFormValues).toEqual(
      expect.objectContaining({
        scopeType: "site",
        scopeId: "Site 101",
        siteId: "101",
      }),
    );
    expect(result.current.historyEvents[0]?.scopeLabel).toBe("Site 101");
  });

  it("estimates observed reduction from confirmed targets when telemetry is absent", async () => {
    const activeEvent = curtailmentEvent({
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        confirmed: 1,
        pending: 1,
        total: 3,
      }),
      targets: [
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.DISPATCHED,
          baselinePowerW: 3000,
        }),
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.CONFIRMED,
          baselinePowerW: 3000,
        }),
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.PENDING,
          baselinePowerW: 3000,
        }),
      ],
      decisionSnapshot: {
        estimated_reduction_kw: 6.2,
        selected_count: 3,
      },
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.observedReductionKw).toBeCloseTo(2.07);
  });

  it("shows the configured restore batch size, not the stale start-time stamp", async () => {
    // effective_batch_size snapshots the selected count at Start; all-paired
    // and closed-loop growth leaves it stale, and the reconciler's restore
    // claims follow restore_batch_size anyway.
    const activeEvent = curtailmentEvent({
      effectiveBatchSize: 10,
      restoreBatchSize: 1,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.restoreBatchSize).toBe(1);
    expect(result.current.activeEventFormValues?.restoreBatchSize).toBe("1");
  });

  it("keeps safety-limit restore semantics when the target set outgrows the start stamp", async () => {
    // Full-shutdown all-paired repro: restore_batch_size=0 stamps
    // effective_batch_size with the paired count at Start (3). Miners paired
    // after the start grow the live target set (50); the restore display must
    // keep the configured "up to safety limit" semantics instead of showing
    // the stale 3-miner stamp as the restore wave size.
    const activeEvent = curtailmentEvent({
      mode: CurtailmentMode.FULL_FLEET,
      forceIncludeAllPairedMiners: true,
      modeParams: { case: "fullFleet", value: create(FullFleetParamsSchema, {}) },
      restoreBatchSize: 0,
      effectiveBatchSize: 3,
      decisionSnapshot: {
        selected_count: 3,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 45,
        pending: 5,
        total: 50,
      }),
      targets: [],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.selectedMiners).toBe(50);
    expect(result.current.activeEvent?.restoreBatchSize).toBe(0);
    expect(result.current.activeEventFormValues?.restoreBatchSize).toBe("");
  });

  it("maps configured curtail batch controls into active event form values", async () => {
    const activeEvent = curtailmentEvent({
      curtailBatchSize: 20,
      curtailBatchIntervalSec: 0,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventFormValues).toEqual(
      expect.objectContaining({
        curtailBatchSize: "20",
        curtailBatchIntervalSec: "0",
      }),
    );
    // The active card's time-to-curtail estimate reads the raw pacing values.
    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        curtailBatchSize: 20,
        curtailBatchIntervalSec: 0,
      }),
    );
  });

  it("maps all-pending events without telemetry to zero observed reduction", async () => {
    const activeEvent = curtailmentEvent({
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 2,
        total: 2,
      }),
      targets: [
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.PENDING,
          baselinePowerW: 3000,
        }),
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.PENDING,
          baselinePowerW: 3000,
        }),
      ],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.observedReductionKw).toBe(0);
  });

  it("uses the active display state for the active history row", async () => {
    const activeEvent = curtailmentEvent({
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        pending: 1,
        total: 2,
      }),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        state: "pending",
        displayState: "curtailing",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("injectedActive");
  });

  it("keeps server-scrubbed history metadata when injecting active display state", async () => {
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-scrubbed-source",
      externalSource: "webhook-secret",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        pending: 1,
        total: 2,
      }),
    });
    const historyEvent = curtailmentEvent({
      eventUuid: "curt-scrubbed-source",
      externalSource: "",
      state: CurtailmentEventState.PENDING,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [historyEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-scrubbed-source",
        displayState: "curtailing",
        sourceLabel: "Manual",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("injectedActive");
  });

  it("preserves the active source label for injected active history rows without a server history match", async () => {
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-unmatched-source",
      externalSource: "Demand response",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-unmatched-source",
        injectedActive: true,
        sourceLabel: "Demand response",
      }),
    );
  });

  it("loads only the first history page and paginates on demand", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "",
      });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(1);
    expect(mockListCurtailmentEvents.mock.calls[0][0]).toEqual(
      expect.objectContaining({ pageSize: 50, pageToken: "" }),
    );
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-1"]);
    expect(result.current.historyCurrentPage).toBe(0);
    expect(result.current.historyHasPreviousPage).toBe(false);
    expect(result.current.historyHasNextPage).toBe(true);

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(2);
    expect(mockListCurtailmentEvents.mock.calls[1][0]).toEqual(
      expect.objectContaining({ pageSize: 50, pageToken: "page-2" }),
    );
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-2"]);
    expect(result.current.historyCurrentPage).toBe(1);
    expect(result.current.historyHasPreviousPage).toBe(true);
    expect(result.current.historyHasNextPage).toBe(false);
  });

  it("stops exposing next history pages when page tokens repeat", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "page-2",
      });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyHasNextPage).toBe(true);

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(2);
    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual(["", "page-2"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-2"]);
    expect(result.current.historyHasNextPage).toBe(false);
  });

  it("sends server history status filters and resets pagination", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "",
      })
      .mockResolvedValueOnce({
        events: [
          curtailmentEvent({
            eventUuid: "curt-completed",
            state: CurtailmentEventState.COMPLETED,
          }),
        ],
        nextPageToken: "",
      });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    await act(async () => {
      await result.current.setHistoryStatusFilters(["completed", "failed"]);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(3);
    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual(["", "page-2", ""]);
    expect(mockListCurtailmentEvents.mock.calls[2][0]).toEqual(
      expect.objectContaining({
        pageSize: 50,
        stateFilters: [CurtailmentEventState.COMPLETED, CurtailmentEventState.FAILED],
      }),
    );
    expect(result.current.historyCurrentPage).toBe(0);
    expect(result.current.historyStatusFilters).toEqual(["completed", "failed"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-completed"]);
  });

  it("does not prepend a non-matching active event to filtered history", async () => {
    const activeEvent = curtailmentEvent({ eventUuid: "curt-active", state: CurtailmentEventState.ACTIVE });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-completed",
      state: CurtailmentEventState.COMPLETED,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["completed"]);
    });

    expect(result.current.activeEventId).toBe("curt-active");
    expect(result.current.historyStatusFilters).toEqual(["completed"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-completed"]);
    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(1);
  });

  it("prepends matching non-selected active events to filtered history", async () => {
    const selectedActiveEvent = curtailmentEvent({
      eventUuid: "curt-selected-active",
      state: CurtailmentEventState.ACTIVE,
    });
    const pendingActiveEvent = curtailmentEvent({
      eventUuid: "curt-pending-active",
      reason: "Pending active event",
      state: CurtailmentEventState.PENDING,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [selectedActiveEvent, pendingActiveEvent] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: selectedActiveEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["pending"]);
    });

    expect(result.current.activeEventId).toBe("curt-selected-active");
    expect(result.current.historyStatusFilters).toEqual(["pending"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-pending-active"]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        reason: "Pending active event",
        state: "pending",
      }),
    );
  });

  it("uses live rollup totals for injected active rows in filtered history", async () => {
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-live-injected",
      state: CurtailmentEventState.ACTIVE,
      decisionSnapshot: {
        estimated_reduction_kw: 6.2,
        selected_count: 10,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 4990,
        dispatched: 10,
        total: 5000,
      }),
      targets: [],
    });
    mockListActiveCurtailments.mockResolvedValue({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["active"]);
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-live-injected",
        state: "active",
        selectedMiners: 5000,
      }),
    );
  });

  it("reconciles terminal active state when current history filters exclude terminal rows", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-filtered-terminal",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-filtered-terminal",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    const newerHistoryEvent = curtailmentEvent({
      eventUuid: "curt-newer-history",
      state: CurtailmentEventState.COMPLETED,
    });
    applyActiveCurtailmentEvent(restoringEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: undefined });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [newerHistoryEvent], nextPageToken: "page-2" })
      .mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(mockListCurtailmentEvents.mock.calls[0][0].stateFilters).toEqual([CurtailmentEventState.RESTORING]);
    const terminalStateFilters = [
      CurtailmentEventState.COMPLETED,
      CurtailmentEventState.COMPLETED_WITH_FAILURES,
      CurtailmentEventState.CANCELLED,
      CurtailmentEventState.FAILED,
    ];
    expect(mockListCurtailmentEvents.mock.calls[1][0].stateFilters).toEqual(terminalStateFilters);
    expect(mockListCurtailmentEvents.mock.calls[2][0].stateFilters).toEqual(terminalStateFilters);
    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual(["", "", "page-2"]);
    expect(result.current.activeEventId).toBe("curt-filtered-terminal");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([]);
  });

  it("preserves other active curtailments when the selected restoring event reconciles terminal", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-restoring",
      state: CurtailmentEventState.RESTORING,
    });
    const otherActiveEvent = curtailmentEvent({
      eventUuid: "curt-other-active",
      reason: "Other active event",
      state: CurtailmentEventState.ACTIVE,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-restoring",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [restoringEvent, otherActiveEvent] });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-restoring");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.activeEvents).toEqual([
      expect.objectContaining({
        id: "curt-restoring",
        state: "completed",
      }),
      expect.objectContaining({
        id: "curt-other-active",
        state: "active",
        reason: "Other active event",
      }),
    ]);
  });

  it("reconciles a vanished selected restoring event terminal while other active curtailments remain", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-restoring",
      state: CurtailmentEventState.RESTORING,
    });
    const otherActiveEvent = curtailmentEvent({
      eventUuid: "curt-other-active",
      reason: "Other active event",
      state: CurtailmentEventState.ACTIVE,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-restoring",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    applyActiveCurtailmentEvent(restoringEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [otherActiveEvent] });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-restoring");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.activeEvents).toEqual([
      expect.objectContaining({
        id: "curt-restoring",
        state: "completed",
      }),
      expect.objectContaining({
        id: "curt-other-active",
        state: "active",
        reason: "Other active event",
      }),
    ]);
  });

  it("preserves a vanished non-selected restoring event until history confirms terminal state", async () => {
    const selectedActiveEvent = curtailmentEvent({
      eventUuid: "curt-selected-active",
      reason: "Selected active event",
      state: CurtailmentEventState.ACTIVE,
    });
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-vanished-restoring",
      reason: "Restoring event",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-vanished-restoring",
      reason: "Restoring event",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    applyActiveCurtailmentEvent(restoringEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(selectedActiveEvent, { mergeActiveEvents: true });
    mockGetCurtailmentEvent.mockImplementation(({ eventUuid }: { eventUuid: string }) =>
      eventUuid === restoringEvent.eventUuid ? { event: restoringEvent } : undefined,
    );
    mockListActiveCurtailments
      .mockResolvedValueOnce({ events: [selectedActiveEvent] })
      .mockResolvedValueOnce({ events: [selectedActiveEvent] })
      .mockResolvedValueOnce({ events: [selectedActiveEvent] });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(result.current.activeEventId).toBe("curt-selected-active");
    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.activeEvents.map((event) => event.id)).toEqual([
      "curt-selected-active",
      "curt-vanished-restoring",
    ]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-vanished-restoring"]);

    await act(async () => {
      await refreshActiveCurtailmentData();
    });

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvents.map((event) => event.id)).toEqual(["curt-selected-active"]);
    expect(result.current.historyEvents).toEqual([]);
    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.stateFilters)).toEqual([
      [CurtailmentEventState.RESTORING],
      [
        CurtailmentEventState.COMPLETED,
        CurtailmentEventState.COMPLETED_WITH_FAILURES,
        CurtailmentEventState.CANCELLED,
        CurtailmentEventState.FAILED,
      ],
      [CurtailmentEventState.RESTORING],
      [
        CurtailmentEventState.COMPLETED,
        CurtailmentEventState.COMPLETED_WITH_FAILURES,
        CurtailmentEventState.CANCELLED,
        CurtailmentEventState.FAILED,
      ],
    ]);
  });

  it("does not preserve a vanished restoring event that is no longer readable", async () => {
    const selectedActiveEvent = curtailmentEvent({
      eventUuid: "curt-selected-active",
      reason: "Selected active event",
      state: CurtailmentEventState.ACTIVE,
    });
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-unreadable-restoring",
      reason: "Restoring event",
      state: CurtailmentEventState.RESTORING,
    });
    applyActiveCurtailmentEvent(restoringEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(selectedActiveEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [selectedActiveEvent] });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [], nextPageToken: "" });
    mockGetCurtailmentEvent.mockImplementation(({ eventUuid }: { eventUuid: string }) => {
      if (eventUuid === restoringEvent.eventUuid) {
        throw new ConnectError("permission denied", Code.PermissionDenied);
      }
      return undefined;
    });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-selected-active");
    expect(result.current.activeEvents.map((event) => event.id)).toEqual(["curt-selected-active"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-selected-active"]);
  });

  it("reconciles a vanished restoring event from a terminal keyed probe", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-terminal-probe",
      reason: "Restoring event",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-terminal-probe",
      reason: "Restoring event",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });
    mockGetCurtailmentEvent.mockImplementation(({ eventUuid }: { eventUuid: string }) =>
      eventUuid === restoringEvent.eventUuid ? { event: completedEvent } : undefined,
    );

    applyActiveCurtailmentEvent(restoringEvent, { mergeActiveEvents: true });
    const { result } = renderHook(() => useCurtailmentApi());
    applyActiveCurtailmentEvent(undefined);

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(result.current.activeEventId).toBe("curt-terminal-probe");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([]);
  });

  it("drops a mutation-backed vanished restoring event when history confirms terminal state", async () => {
    const selectedActiveEvent = curtailmentEvent({
      eventUuid: "curt-selected-active",
      reason: "Selected active event",
      state: CurtailmentEventState.ACTIVE,
    });
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-mutation-restoring",
      reason: "Restoring event",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-mutation-restoring",
      reason: "Restoring event",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    applyActiveCurtailmentEvent(restoringEvent, {
      mergeActiveEvents: true,
      preserveAgainstStaleRefresh: true,
    });
    applyActiveCurtailmentEvent(selectedActiveEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [selectedActiveEvent] });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-selected-active");
    expect(result.current.activeEvents.map((event) => event.id)).toEqual(["curt-selected-active"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual([
      "curt-selected-active",
      "curt-mutation-restoring",
    ]);
  });

  it("reconciles restoring state from terminal history without resetting the current page", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-page-terminal",
      state: CurtailmentEventState.RESTORING,
    });
    const currentPageEvent = curtailmentEvent({
      eventUuid: "curt-current-page",
      state: CurtailmentEventState.COMPLETED,
    });
    const unrelatedTerminalEvent = curtailmentEvent({
      eventUuid: "curt-unrelated-terminal",
      state: CurtailmentEventState.COMPLETED,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-page-terminal",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockListActiveCurtailments
      .mockResolvedValueOnce({ event: restoringEvent })
      .mockResolvedValueOnce({ event: restoringEvent })
      .mockResolvedValueOnce({ event: undefined });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [restoringEvent], nextPageToken: "page-2" })
      .mockResolvedValueOnce({ events: [currentPageEvent], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [unrelatedTerminalEvent], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [currentPageEvent], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });
    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    expect(result.current.historyCurrentPage).toBe(1);
    expect(result.current.activeEvent?.state).toBe("restoring");

    await act(async () => {
      await result.current.refreshCurtailment({ background: true });
    });

    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual([
      "",
      "page-2",
      "",
      "page-2",
      "",
    ]);
    expect(result.current.historyCurrentPage).toBe(1);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-current-page"]);
    expect(result.current.activeEventId).toBe("curt-page-terminal");
    expect(result.current.activeEvent?.state).toBe("completed");
  });

  it("caps terminal reconciliation history paging when the active row is not found", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-missing-terminal",
      state: CurtailmentEventState.RESTORING,
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListActiveCurtailments.mockResolvedValueOnce({ event: undefined });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [curtailmentEvent({ eventUuid: "curt-terminal-1" })], nextPageToken: "page-2" })
      .mockResolvedValueOnce({ events: [curtailmentEvent({ eventUuid: "curt-terminal-2" })], nextPageToken: "page-3" })
      .mockResolvedValueOnce({ events: [curtailmentEvent({ eventUuid: "curt-terminal-3" })], nextPageToken: "page-4" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual([
      "",
      "",
      "page-2",
      "page-3",
    ]);
    expect(result.current.activeEvent?.state).toBe("restoring");
  });

  it("refreshes history without refetching active curtailment when requested", async () => {
    const activeEvent = curtailmentEvent({ eventUuid: "curt-active" });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-completed",
      state: CurtailmentEventState.COMPLETED,
    });
    applyActiveCurtailmentEvent(activeEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(mockListActiveCurtailments).not.toHaveBeenCalled();
    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(1);
    expect(result.current.activeEventId).toBe("curt-active");
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-active", "curt-completed"]);
  });

  it("keeps a newly started curtailment selected when active refresh returns another event first", async () => {
    const previousActiveEvent = curtailmentEvent({
      eventUuid: "curt-previous-active",
      reason: "Previous active event",
    });
    const startedEvent = curtailmentEvent({
      eventUuid: "curt-new-active",
      reason: "New active event",
    });
    applyActiveCurtailmentEvent(previousActiveEvent, { mergeActiveEvents: true });
    mockStartCurtailment.mockResolvedValueOnce({ event: startedEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [], nextPageToken: "" });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [previousActiveEvent, startedEvent] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: startedEvent });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.startCurtailment(baseSubmitValues);
    });
    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-new-active");
    expect(result.current.activeEvent?.reason).toBe("New active event");
    expect(result.current.activeEvents.map((event) => event.id)).toEqual(["curt-previous-active", "curt-new-active"]);
  });

  it("keeps a newly started pending curtailment visible while active and history refreshes are stale", async () => {
    const startedEvent = curtailmentEvent({
      eventUuid: "curt-new-pending",
      reason: "New pending event",
      state: CurtailmentEventState.PENDING,
      decisionSnapshot: {
        estimated_reduction_kw: 6.6,
        selected_count: 2,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 2,
        total: 2,
      }),
    });
    const olderHistoryEvent = curtailmentEvent({
      eventUuid: "curt-yesterday",
      reason: "Older completed event",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-04-30T13:00:00Z"),
    });
    mockStartCurtailment.mockResolvedValueOnce({ event: startedEvent });
    mockListActiveCurtailments.mockResolvedValue({ event: undefined });
    mockListCurtailmentEvents.mockResolvedValue({ events: [olderHistoryEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.startCurtailment(baseSubmitValues);
    });

    expect(result.current.activeEventId).toBe("curt-new-pending");
    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        reason: "New pending event",
        state: "pending",
        selectedMiners: 2,
        estimatedReductionKw: 6.6,
        targetKw: 5,
      }),
    );

    await act(async () => {
      await result.current.refreshCurtailment({ background: true });
    });
    await act(async () => {
      await result.current.refreshCurtailment({ background: true });
    });
    await act(async () => {
      await result.current.refreshCurtailment({ background: true });
    });

    expect(result.current.activeEventId).toBe("curt-new-pending");
    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        reason: "New pending event",
        state: "pending",
        selectedMiners: 2,
        estimatedReductionKw: 6.6,
        targetKw: 5,
      }),
    );
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-new-pending", "curt-yesterday"]);
  });

  it("removes a failed mutation event without clearing unrelated active curtailments", async () => {
    const otherActiveEvent = curtailmentEvent({
      eventUuid: "curt-other-active",
      reason: "Other active event",
    });
    const failedEvent = curtailmentEvent({
      eventUuid: "curt-failed",
      reason: "Failed event",
      state: CurtailmentEventState.FAILED,
    });
    applyActiveCurtailmentEvent(otherActiveEvent, { mergeActiveEvents: true });
    mockUpdateCurtailment.mockResolvedValueOnce({ event: failedEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [failedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.updateCurtailment("curt-failed", baseSubmitValues, baseSubmitValues);
    });

    expect(result.current.activeEventId).toBe("curt-other-active");
    expect(result.current.activeEvent?.reason).toBe("Other active event");
    expect(result.current.activeEvents.map((event) => event.id)).toEqual(["curt-other-active"]);
    expect(result.current.historyEvents).toContainEqual(
      expect.objectContaining({
        id: "curt-failed",
        state: "failed",
      }),
    );
  });

  it("does not apply active reconciliation from superseded history refreshes", async () => {
    const activeEvent = curtailmentEvent({ eventUuid: "curt-superseded" });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-superseded",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 2,
        total: 2,
      }),
      targets: [],
    });
    let resolveSupersededHistory: (value: { events: CurtailmentEvent[]; nextPageToken: string }) => void = () =>
      undefined;
    mockListCurtailmentEvents
      .mockReturnValueOnce(
        new Promise<{ events: CurtailmentEvent[]; nextPageToken: string }>((resolve) => {
          resolveSupersededHistory = resolve;
        }),
      )
      .mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });
    applyActiveCurtailmentEvent(activeEvent);

    const { result } = renderHook(() => useCurtailmentApi());

    let supersededRefresh!: Promise<unknown>;
    act(() => {
      supersededRefresh = result.current.refreshCurtailment({ includeActive: false });
    });
    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEvent?.state).toBe("active");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-superseded",
        state: "active",
      }),
    );

    resolveSupersededHistory({ events: [completedEvent], nextPageToken: "" });
    await act(async () => {
      await supersededRefresh;
    });

    expect(result.current.activeEvent?.state).toBe("active");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-superseded",
        state: "active",
      }),
    );
  });

  it("does not commit preserved restoring state from superseded refreshes", async () => {
    const staleActiveEvent = curtailmentEvent({ eventUuid: "curt-selected", reason: "Stale selected" });
    const freshActiveEvent = curtailmentEvent({ eventUuid: "curt-selected", reason: "Fresh selected" });
    const vanishedRestoringEvent = curtailmentEvent({
      eventUuid: "curt-vanished",
      reason: "Vanished restoring",
      state: CurtailmentEventState.RESTORING,
    });
    let resolveStaleProbe: (value: { event: CurtailmentEvent }) => void = () => undefined;
    let vanishedProbeCalls = 0;
    mockGetCurtailmentEvent.mockImplementation(({ eventUuid }: { eventUuid: string }) => {
      if (eventUuid !== vanishedRestoringEvent.eventUuid) {
        return undefined;
      }
      vanishedProbeCalls += 1;
      if (vanishedProbeCalls === 1) {
        return new Promise<{ event: CurtailmentEvent }>((resolve) => {
          resolveStaleProbe = resolve;
        });
      }
      throw new ConnectError("permission denied", Code.PermissionDenied);
    });
    applyActiveCurtailmentEvent(vanishedRestoringEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(staleActiveEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments
      .mockResolvedValueOnce({ events: [staleActiveEvent] })
      .mockResolvedValueOnce({ events: [freshActiveEvent] });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [freshActiveEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    let staleRefresh!: Promise<unknown>;
    act(() => {
      staleRefresh = result.current.refreshCurtailment();
    });
    await vi.waitFor(() => expect(vanishedProbeCalls).toBe(1));

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    resolveStaleProbe({ event: vanishedRestoringEvent });
    await act(async () => {
      await staleRefresh;
    });

    expect(result.current.activeEvent?.reason).toBe("Fresh selected");
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-selected"]);
  });

  it("uses the shared active curtailment snapshot for active fields and current history", async () => {
    const pendingEvent = curtailmentEvent({
      eventUuid: "curt-shared",
      reason: "Queued dispatch",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 2,
        total: 2,
      }),
    });
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-shared",
      reason: "Dispatch started",
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 2,
        total: 2,
      }),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: pendingEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [pendingEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.reason).toBe("Queued dispatch");
    expect(result.current.historyEvents[0].reason).toBe("Queued dispatch");

    act(() => {
      applyActiveCurtailmentEvent(activeEvent);
    });

    expect(result.current.activeEvent?.reason).toBe("Dispatch started");
    expect(result.current.historyEvents[0].reason).toBe("Dispatch started");
  });

  it("replaces cached non-terminal history rows when the shared active event becomes terminal", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-terminal-shared",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-terminal-shared",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [restoringEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0].state).toBe("restoring");

    act(() => {
      applyActiveCurtailmentEvent(completedEvent);
    });

    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-terminal-shared",
        state: "completed",
        endedAt: "2026-05-01T13:00:00.000Z",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("displayState");
    expect(result.current.historyEvents[0]).not.toHaveProperty("injectedActive");
  });

  it("drops stale injected active history rows when the shared active event changes", async () => {
    const pendingEvent = curtailmentEvent({
      eventUuid: "curt-shared-a",
      reason: "Queued dispatch",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        pending: 1,
        total: 2,
      }),
    });
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-shared-b",
      reason: "Dispatch started",
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: pendingEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents).toEqual([
      expect.objectContaining({
        id: "curt-shared-a",
        displayState: "curtailing",
      }),
    ]);

    act(() => {
      applyActiveCurtailmentEvent(activeEvent);
    });

    expect(result.current.activeEventId).toBe("curt-shared-b");
    expect(result.current.historyEvents).toEqual([
      expect.objectContaining({
        id: "curt-shared-b",
      }),
    ]);
  });

  it("removes injected active history rows when shared active state stops matching filters", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-filtered-active",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-filtered-active",
      state: CurtailmentEventState.COMPLETED,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-filtered-active"]);

    act(() => {
      applyActiveCurtailmentEvent(completedEvent);
    });

    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([]);
  });

  it("keeps real history rows when removing stale injected active rows from filtered history", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-injected",
      state: CurtailmentEventState.RESTORING,
    });
    const realHistoryEvent = curtailmentEvent({
      eventUuid: "curt-real-history",
      reason: "Real history row",
      state: CurtailmentEventState.RESTORING,
    });
    const filteredActiveEvent = curtailmentEvent({
      eventUuid: "curt-real-history",
      state: CurtailmentEventState.PENDING,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [realHistoryEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-injected", "curt-real-history"]);

    act(() => {
      applyActiveCurtailmentEvent(filteredActiveEvent);
    });

    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([
      expect.objectContaining({
        id: "curt-real-history",
        reason: "Real history row",
      }),
    ]);
  });

  it("keeps server-backed active history rows when shared active state stops matching filters", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-server-backed",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-server-backed",
      state: CurtailmentEventState.COMPLETED,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [restoringEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-server-backed",
        displayState: "restoring",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("injectedActive");

    act(() => {
      applyActiveCurtailmentEvent(completedEvent);
    });

    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([
      expect.objectContaining({
        id: "curt-server-backed",
        state: "restoring",
      }),
    ]);
  });

  it("keeps a restored curtailment visible until it is dismissed", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-restored",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    const restoredEvent = curtailmentEvent({
      eventUuid: "curt-restored",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      decisionSnapshot: undefined,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 2,
        total: 2,
      }),
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [restoredEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBe("curt-restored");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.activeEvent?.endedAt).toBe("2026-05-01T13:00:00.000Z");
    expect(result.current.activeEvent?.remainingPowerKw).toBe(1);
    expect(result.current.activeEvent?.rollups).toEqual([{ state: "resolved", count: 2 }]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-restored",
        state: "completed",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("displayState");

    act(() => {
      result.current.dismissTerminalCurtailment();
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
  });

  it("keeps a restored curtailment visible when terminal history arrives after active goes empty", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-delayed-terminal-history",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 2,
        total: 2,
      }),
    });
    const restoredEvent = curtailmentEvent({
      eventUuid: "curt-delayed-terminal-history",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 2,
        total: 2,
      }),
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListActiveCurtailments.mockResolvedValue({ event: undefined });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [restoredEvent], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [restoredEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-delayed-terminal-history");
    expect(result.current.activeEvent?.state).toBe("restoring");

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-delayed-terminal-history");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.activeEvent?.endedAt).toBe("2026-05-01T13:00:00.000Z");
    expect(result.current.activeEvent?.rollups).toEqual([{ state: "resolved", count: 2 }]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-delayed-terminal-history",
        state: "completed",
      }),
    );

    act(() => {
      result.current.dismissTerminalCurtailment();
    });

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
  });

  it("keeps restoring visible while active is empty and history is stale", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-stale-pending-history",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 2,
        total: 2,
      }),
    });
    const stalePendingHistoryEvent = curtailmentEvent({
      eventUuid: "curt-stale-pending-history",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 2,
        total: 2,
      }),
    });
    const restoredEvent = curtailmentEvent({
      eventUuid: "curt-stale-pending-history",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 2,
        total: 2,
      }),
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListActiveCurtailments.mockResolvedValue({ event: undefined });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [stalePendingHistoryEvent], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [stalePendingHistoryEvent], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [restoredEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-stale-pending-history");
    expect(result.current.activeEvent?.state).toBe("restoring");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-stale-pending-history",
        state: "restoring",
        displayState: "restoring",
      }),
    );

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-stale-pending-history");
    expect(result.current.activeEvent?.state).toBe("restoring");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-stale-pending-history",
        state: "restoring",
        displayState: "restoring",
      }),
    );

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-stale-pending-history");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-stale-pending-history",
        state: "completed",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("displayState");
  });

  it("keeps restoring visible when a shared active-only poll clears the cache during history refresh", async () => {
    let resolveHistory: (value: { events: CurtailmentEvent[]; nextPageToken: string }) => void = () => undefined;
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-shared-active-race",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 2,
        total: 2,
      }),
    });
    const stalePendingHistoryEvent = curtailmentEvent({
      eventUuid: "curt-shared-active-race",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 2,
        total: 2,
      }),
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListActiveCurtailments.mockResolvedValue({ event: undefined });
    mockListCurtailmentEvents.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveHistory = resolve;
      }),
    );

    const { result } = renderHook(() => useCurtailmentApi());
    let refreshPromise: Promise<unknown> = Promise.resolve();

    act(() => {
      refreshPromise = result.current.refreshCurtailment();
    });
    await act(async () => {
      await refreshActiveCurtailmentData();
    });
    await act(async () => {
      resolveHistory({ events: [stalePendingHistoryEvent], nextPageToken: "" });
      await refreshPromise;
    });

    expect(result.current.activeEventId).toBe("curt-shared-active-race");
    expect(result.current.activeEvent?.state).toBe("restoring");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-shared-active-race",
        state: "restoring",
        displayState: "restoring",
      }),
    );
  });

  it("keeps an incomplete restore visible until it is dismissed", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-restore-incomplete",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    const restoreIncompleteEvent = curtailmentEvent({
      eventUuid: "curt-restore-incomplete",
      state: CurtailmentEventState.COMPLETED_WITH_FAILURES,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      decisionSnapshot: undefined,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 1,
        restoreFailed: 1,
        total: 2,
      }),
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [restoreIncompleteEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBe("curt-restore-incomplete");
    expect(result.current.activeEvent?.state).toBe("completedWithFailures");
    expect(result.current.activeEvent?.remainingPowerKw).toBe(1);
    expect(result.current.activeEvent?.rollups).toEqual([
      { state: "resolved", count: 1 },
      { state: "restoreFailed", count: 1 },
    ]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-restore-incomplete",
        state: "completedWithFailures",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("displayState");

    act(() => {
      result.current.dismissTerminalCurtailment();
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
  });

  it("does not preserve stale restoring rollups when terminal history is trimmed", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-trimmed-restore",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    const trimmedTerminalEvent = curtailmentEvent({
      eventUuid: "curt-trimmed-restore",
      state: CurtailmentEventState.COMPLETED_WITH_FAILURES,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      targetRollup: undefined,
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [trimmedTerminalEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBe("curt-trimmed-restore");
    expect(result.current.activeEvent?.state).toBe("completedWithFailures");
    expect(result.current.activeEvent?.rollups).toEqual([]);
    expect(result.current.activeEvent?.remainingPowerKw).toBeUndefined();
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-trimmed-restore",
        state: "completedWithFailures",
      }),
    );
  });

  it("clears an active snapshot when history reports a failed terminal event", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-failed",
      state: CurtailmentEventState.RESTORING,
    });
    const failedEvent = curtailmentEvent({
      eventUuid: "curt-failed",
      state: CurtailmentEventState.FAILED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      decisionSnapshot: undefined,
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [failedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-failed",
        state: "failed",
      }),
    );
  });

  it("keeps non-first history pages stable when mutation refresh fails", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "",
      })
      .mockRejectedValueOnce(new Error("refresh failed"));
    mockStartCurtailment.mockResolvedValueOnce({ event: curtailmentEvent({ eventUuid: "curt-new" }) });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    await act(async () => {
      await result.current.startCurtailment(baseSubmitValues);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(3);
    expect(result.current.activeEventId).toBe("curt-new");
    expect(result.current.historyCurrentPage).toBe(1);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-2"]);
    expect(result.current.loadError).toBe("refresh failed");
  });

  it("clears a detail load error after a later active selection succeeds", async () => {
    const activeSummary = curtailmentEvent({ eventUuid: "curt-detail-load", reason: "Summary" });
    const activeDetail = curtailmentEvent({ eventUuid: "curt-detail-load", reason: "Detail" });
    applyActiveCurtailmentEvent(activeSummary, { mergeActiveEvents: true });
    mockGetCurtailmentEvent.mockRejectedValueOnce(new Error("detail failed")).mockResolvedValueOnce({
      event: activeDetail,
    });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.selectActiveCurtailment("curt-detail-load").catch(() => undefined);
    });

    expect(result.current.loadError).toBe("detail failed");

    await act(async () => {
      await result.current.selectActiveCurtailment("curt-detail-load");
    });

    expect(result.current.activeEvent?.reason).toBe("Detail");
    expect(result.current.loadError).toBeNull();
  });

  it("passes refresh abort signals to history requests and uses a shared active request signal", async () => {
    const abortController = new AbortController();

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ signal: abortController.signal });
    });

    expect(mockListActiveCurtailments.mock.calls[0][1]).toEqual({ signal: expect.any(AbortSignal) });
    expect(mockListCurtailmentEvents.mock.calls[0][1]).toEqual({ signal: abortController.signal });
  });

  it("starts and stops curtailment with refresh events", async () => {
    const changedListener = vi.fn();
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
    const startedEvent = curtailmentEvent();
    const restoringEvent = curtailmentEvent({
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    mockStartCurtailment.mockResolvedValueOnce({ event: startedEvent });
    mockStopCurtailment.mockResolvedValueOnce({ event: restoringEvent });
    mockListActiveCurtailments.mockResolvedValue({ event: startedEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [startedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.startCurtailment(baseSubmitValues);
    });

    expect(mockStartCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        reason: "Grid peak",
        mode: CurtailmentMode.FIXED_KW,
      }),
    );
    expect(changedListener).toHaveBeenCalledTimes(1);
    expect(result.current.activeEvent?.state).toBe("active");

    mockListActiveCurtailments.mockResolvedValue({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [restoringEvent], nextPageToken: "" });

    await act(async () => {
      await result.current.stopCurtailment("curt-1");
    });

    expect(mockStopCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        force: false,
      }),
    );
    expect(changedListener).toHaveBeenCalledTimes(2);
    expect(result.current.activeEvent?.state).toBe("restoring");

    window.removeEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
  });

  it("force stops curtailment when requested", async () => {
    const restoringEvent = curtailmentEvent({
      state: CurtailmentEventState.RESTORING,
    });
    mockStopCurtailment.mockResolvedValueOnce({ event: restoringEvent });
    mockListActiveCurtailments.mockResolvedValue({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [restoringEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.stopCurtailment("curt-1", { force: true });
    });

    expect(mockStopCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        force: true,
      }),
    );
    expect(result.current.activeEvent?.state).toBe("restoring");
  });

  it("admin terminates restoring events with a required reason", async () => {
    const cancelledEvent = curtailmentEvent({
      state: CurtailmentEventState.CANCELLED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockAdminTerminateEvent.mockResolvedValueOnce({ event: cancelledEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [cancelledEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await expect(
        result.current.adminTerminateCurtailment("curt-1", {
          reason: "   ",
          targetState: "cancelled",
        }),
      ).rejects.toThrow("Enter a reason before terminating the event.");
    });

    expect(mockAdminTerminateEvent).not.toHaveBeenCalled();
    expect(result.current.adminTerminateError).toBe("Enter a reason before terminating the event.");

    await act(async () => {
      await result.current.adminTerminateCurtailment("curt-1", {
        reason: " Operator recovered stale source ",
        targetState: "cancelled",
      });
    });

    expect(mockAdminTerminateEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        reason: "Operator recovered stale source",
        targetState: CurtailmentEventState.CANCELLED,
      }),
    );
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-1",
        state: "cancelled",
      }),
    );
  });

  it("admin terminates restoring events as failed", async () => {
    const failedEvent = curtailmentEvent({
      state: CurtailmentEventState.FAILED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockAdminTerminateEvent.mockResolvedValueOnce({ event: failedEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [failedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.adminTerminateCurtailment("curt-1", {
        reason: "Operator marked restore failed",
        targetState: "failed",
      });
    });

    expect(mockAdminTerminateEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        reason: "Operator marked restore failed",
        targetState: CurtailmentEventState.FAILED,
      }),
    );
  });

  it("force releases curtailment ownership with a required reason", async () => {
    const releasedEvent = curtailmentEvent({
      state: CurtailmentEventState.CANCELLED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockForceReleaseCurtailmentOwnership.mockResolvedValueOnce({
      event: releasedEvent,
      releasedTargetCount: 17,
      ownershipReleased: true,
      restoreAttempted: false,
      automationDisabled: true,
    });
    mockListCurtailmentEvents.mockResolvedValue({ events: [releasedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await expect(
        result.current.forceReleaseCurtailment("curt-1", {
          reason: "   ",
        }),
      ).rejects.toThrow("Enter a reason before terminating the event.");
    });

    expect(mockForceReleaseCurtailmentOwnership).not.toHaveBeenCalled();

    let releaseResult: Awaited<ReturnType<typeof result.current.forceReleaseCurtailment>> | undefined;
    await act(async () => {
      releaseResult = await result.current.forceReleaseCurtailment("curt-1", {
        reason: " Release for manual control ",
      });
    });

    expect(mockForceReleaseCurtailmentOwnership).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        reason: "Release for manual control",
      }),
    );
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-1",
        state: "cancelled",
      }),
    );
    expect(releaseResult).toEqual({
      event: releasedEvent,
      releasedTargetCount: 17,
      ownershipReleased: true,
      restoreAttempted: false,
      automationDisabled: true,
    });
  });

  it("starts full-fleet curtailment without fixed-kW mode params", async () => {
    const startedEvent = curtailmentEvent({ mode: CurtailmentMode.FULL_FLEET, modeParams: { case: undefined } });
    mockStartCurtailment.mockResolvedValueOnce({ event: startedEvent });
    mockListActiveCurtailments.mockResolvedValue({ event: startedEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [startedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.startCurtailment({
        ...baseSubmitValues,
        curtailmentMode: "fullFleet",
        targetKw: "",
        toleranceKw: "",
      });
    });

    expect(mockStartCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: CurtailmentMode.FULL_FLEET,
        modeParams: expect.objectContaining({ case: undefined }),
      }),
    );
  });

  it("updates active curtailment fields and refreshes listeners", async () => {
    const changedListener = vi.fn();
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
    const updatedEvent = curtailmentEvent({ reason: "Updated grid peak", restoreBatchIntervalSec: 120 });
    mockUpdateCurtailment.mockResolvedValueOnce({ event: updatedEvent });
    mockListActiveCurtailments.mockResolvedValue({ event: updatedEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [updatedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.updateCurtailment(
        "curt-1",
        {
          ...baseSubmitValues,
          reason: " Updated grid peak ",
          maxDurationSec: "1800",
          restoreBatchSize: "",
          restoreIntervalSec: "120",
        },
        {
          ...baseSubmitValues,
          reason: "Grid peak",
          maxDurationSec: "",
          restoreBatchSize: "",
          restoreIntervalSec: "60",
        },
      );
    });

    const updateRequest = mockUpdateCurtailment.mock.calls[0][0];
    expect(mockUpdateCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        reason: "Updated grid peak",
        maxDurationSeconds: 1800,
        restoreBatchIntervalSec: 120,
      }),
    );
    expect(updateRequest.restoreBatchSize).toBeUndefined();
    expect(changedListener).toHaveBeenCalledTimes(1);
    expect(result.current.activeEvent?.reason).toBe("Updated grid peak");
    expect(result.current.activeEventFormValues?.restoreIntervalSec).toBe("120");

    window.removeEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
  });

  it("surfaces update failures without refreshing listeners", async () => {
    const changedListener = vi.fn();
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
    mockUpdateCurtailment.mockRejectedValueOnce(new Error("rpc failed"));

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await expect(result.current.updateCurtailment("curt-1", baseSubmitValues, baseSubmitValues)).rejects.toThrow(
        "rpc failed",
      );
    });

    expect(result.current.updateError).toBe("rpc failed");
    expect(changedListener).not.toHaveBeenCalled();

    window.removeEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
  });
});
