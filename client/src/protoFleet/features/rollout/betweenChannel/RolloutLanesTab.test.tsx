import { MemoryRouter, useLocation, useSearchParams } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import userEvent from "@testing-library/user-event";

import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import { BETWEEN_CHANNEL_STRATEGY_KEY } from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import type {
  RolloutLane,
  RolloutLaneTopologyReadiness,
  RolloutMemberState,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";

const listFirmwareFiles = vi.hoisted(() => vi.fn());
const pushToastMock = vi.hoisted(() => vi.fn());
const rolloutApi = vi.hoisted(() => ({
  lane: null as RolloutLane | null,
  lanes: [] as RolloutLane[],
  rollouts: [] as RolloutRecord[],
  isLoading: false,
  isMutating: false,
  loadError: null as string | null,
  mutationError: null as string | null,
  topologyReadiness: null as RolloutLaneTopologyReadiness | null,
  isTopologyReadinessLoading: false,
  topologyReadinessError: null as string | null,
  topologyReadinessForbidden: false,
  topologyReadinessStale: false,
  permissions: {
    canReadChannels: true,
    canManageChannels: false,
    canRead: true,
    canManage: false,
    canControl: false,
  },
  listRolloutLanes: vi.fn(),
  getRolloutLane: vi.fn(),
  listRolloutLaneMembers: vi.fn(),
  previewRolloutLaneMembershipChange: vi.fn(),
  updateRolloutLaneMembership: vi.fn(),
  previewRolloutLaneModelDeclaration: vi.fn(),
  createRolloutLaneModelDeclaration: vi.fn(),
  publishRolloutLaneModelTarget: vi.fn(),
  previewRolloutLaneModelMembershipChange: vi.fn(),
  updateRolloutLaneModelMembership: vi.fn(),
  previewRolloutLane: vi.fn(),
  createRolloutLane: vi.fn(),
  deleteRolloutLane: vi.fn(),
  getRolloutLaneTopologyReadiness: vi.fn(),
  repairRolloutLaneModelBinding: vi.fn(),
  enableRolloutLaneModelTopology: vi.fn(),
  startRolloutLane: vi.fn(),
  listRollouts: vi.fn(),
  getRollout: vi.fn(),
  getRolloutGroup: vi.fn(),
  admitRollout: vi.fn(),
  continueRollout: vi.fn(),
  pauseRollout: vi.fn(),
  resumeRollout: vi.fn(),
  abortRollout: vi.fn(),
  revertRollout: vi.fn(),
  completeRollout: vi.fn(),
}));
const capturedTable = vi.hoisted(() => ({
  onStart: null as ((lane: RolloutLane) => void) | null,
  onSetup: null as ((lane: RolloutLane) => void) | null,
  onManageMembers: null as ((lane: RolloutLane) => void) | null,
  onManageDeclarations: null as ((lane: RolloutLane) => void) | null,
  onView: null as ((rollout: RolloutRecord) => void) | null,
  onDelete: null as ((lane: RolloutLane) => void) | null,
}));

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({ listFirmwareFiles }),
}));

vi.mock("@/protoFleet/api/useRolloutApi", () => ({
  useRolloutApi: () => rolloutApi,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: pushToastMock,
  STATUSES: {
    error: "error",
    success: "success",
  },
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/RolloutLanesTable", () => ({
  default: ({
    rows,
    canStart,
    canDelete,
    deletePermissionBlockedReason,
    isPreparingStart,
    onSetup,
    onManageMembers,
    onManageDeclarations,
    onStart,
    onView,
    onDelete,
  }: {
    rows: Array<{ lane: RolloutLane; latestRollout?: RolloutRecord }>;
    canStart: boolean;
    canDelete: boolean;
    deletePermissionBlockedReason?: string;
    isPreparingStart?: boolean;
    onSetup: (lane: RolloutLane) => void;
    onManageMembers: (lane: RolloutLane) => void;
    onManageDeclarations: (lane: RolloutLane) => void;
    onStart: (lane: RolloutLane) => void;
    onView: (rollout: RolloutRecord) => void;
    onDelete: (lane: RolloutLane) => void;
  }) => {
    capturedTable.onStart = onStart;
    capturedTable.onSetup = onSetup;
    capturedTable.onManageMembers = onManageMembers;
    capturedTable.onManageDeclarations = onManageDeclarations;
    capturedTable.onView = onView;
    capturedTable.onDelete = onDelete;
    return (
      <div data-testid="rollout-lanes-table">
        {rows.map(({ lane, latestRollout }) => (
          <div key={lane.id}>
            {lane.label}
            <button type="button" onClick={() => onSetup(lane)}>
              Setup {lane.label}
            </button>
            <button type="button" onClick={() => onManageMembers(lane)}>
              Members {lane.label}
            </button>
            <button type="button" onClick={() => onManageDeclarations(lane)}>
              Declarations {lane.label}
            </button>
            {canStart ? (
              <button type="button" disabled={isPreparingStart} onClick={() => onStart(lane)}>
                Start {lane.label}
              </button>
            ) : null}
            {latestRollout ? (
              <button type="button" onClick={() => onView(latestRollout)}>
                View {latestRollout.name}
              </button>
            ) : null}
            {canDelete ? (
              <>
                <button
                  type="button"
                  disabled={deletePermissionBlockedReason !== undefined}
                  onClick={() => onDelete(lane)}
                >
                  Delete {lane.label}
                </button>
                {deletePermissionBlockedReason ? <span>{deletePermissionBlockedReason}</span> : null}
              </>
            ) : null}
          </div>
        ))}
      </div>
    );
  },
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/ManageRolloutLaneDeclarationsModal", () => ({
  default: ({ lane, onUpdated }: { lane: RolloutLane; onUpdated: (lane: RolloutLane, message: string) => void }) => (
    <button type="button" onClick={() => onUpdated(lane, `Updated ${lane.label} declarations`)}>
      Complete declaration mutation
    </button>
  ),
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/DeleteRolloutLaneDialog", () => ({
  default: ({
    laneLabel,
    error,
    onConfirm,
    onDismiss,
  }: {
    laneLabel: string;
    error?: string | null;
    onConfirm: () => void;
    onDismiss: () => void;
  }) => (
    <div>
      <span>Delete dialog for {laneLabel}</span>
      {error ? <span>{error}</span> : null}
      <button type="button" onClick={onConfirm}>
        Confirm delete lane
      </button>
      <button type="button" onClick={onDismiss}>
        Cancel delete lane
      </button>
    </div>
  ),
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/ManageRolloutLaneMembersModal", () => ({
  default: ({
    lane,
    canManage,
    onDismiss,
    onUpdated,
  }: {
    lane: RolloutLane;
    canManage: boolean;
    onDismiss: () => void;
    onUpdated: (result: { lane: RolloutLane; transitionMembers: Array<{ deviceIdentifier: string }> }) => void;
  }) => (
    <div>
      <span>Manage members for {lane.label}</span>
      <span>{canManage ? "Membership manager" : "Membership read only"}</span>
      <button type="button" onClick={onDismiss}>
        Close membership
      </button>
      {canManage ? (
        <>
          <button
            type="button"
            onClick={() => {
              const updatedLane = {
                ...lane,
                revision: lane.revision + 1n,
                firmwareConvergence: {
                  ...lane.firmwareConvergence,
                  totalCount: lane.firmwareConvergence.totalCount + 1,
                  pendingCount: 1,
                  members: [
                    {
                      deviceIdentifier: "miner-new",
                      manufacturer: "Proto",
                      model: "Alpha",
                      latestObservedFirmwareVersion: "1.0.0",
                      targetFirmwareVersion: "2.0.0",
                      state: "pending" as const,
                    },
                  ],
                },
              };
              rolloutApi.lane = updatedLane;
              onUpdated({
                lane: updatedLane,
                transitionMembers: [{ deviceIdentifier: "miner-new" }],
              });
            }}
          >
            Complete membership update with transition
          </button>
          <button
            type="button"
            onClick={() => onUpdated({ lane: { ...lane, revision: lane.revision + 1n }, transitionMembers: [] })}
          >
            Complete pure removal
          </button>
        </>
      ) : null}
    </div>
  ),
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/CreateRolloutLaneModal", () => ({
  default: ({
    onCreate,
  }: {
    onCreate: (values: {
      label: string;
      description: string;
      firmwareFileIds: string[];
      deviceIdentifiers: string[];
      confirmInitialEnforcement: boolean;
    }) => void;
  }) => (
    <button
      type="button"
      onClick={() =>
        onCreate({
          label: "New stable lane",
          description: "",
          firmwareFileIds: ["firmware-1"],
          deviceIdentifiers: ["miner-1"],
          confirmInitialEnforcement: true,
        })
      }
    >
      Submit lane creation
    </button>
  ),
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/LaneFirmwareConvergenceStatus", () => ({
  default: ({ lane, onClose, onStart }: { lane: RolloutLane; onClose?: () => void; onStart?: () => void }) => (
    <div>
      <span>Firmware convergence for {lane.label}</span>
      <span data-testid="setup-member-states">
        {lane.firmwareConvergence.members.map((member) => `${member.deviceIdentifier}:${member.state}`).join(",")}
      </span>
      {onClose ? (
        <button type="button" onClick={onClose}>
          Close setup
        </button>
      ) : null}
      {onStart ? (
        <button type="button" onClick={onStart}>
          Start ready lane
        </button>
      ) : null}
    </div>
  ),
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/StartRolloutLaneModal", () => ({
  default: ({
    lane,
    onStart,
  }: {
    lane: RolloutLane;
    onStart: (values: { laneId: string; name: string; firmwareFileIds: string[]; batches: []; reason: string }) => void;
  }) => (
    <div>
      Prepared {lane.label}
      <button
        type="button"
        onClick={() =>
          onStart({
            laneId: lane.id,
            name: `${lane.label} rollout`,
            firmwareFileIds: ["firmware-2"],
            batches: [],
            reason: "Ready",
          })
        }
      >
        Confirm start {lane.label}
      </button>
    </div>
  ),
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/BetweenChannelRolloutStatus", () => ({
  default: ({
    rollout,
    isMutating,
    announceEvidenceStatus,
    onAdmit,
    onContinue,
    onCompleteWithFailures,
  }: {
    rollout: RolloutRecord;
    isMutating?: boolean;
    announceEvidenceStatus?: boolean;
    onAdmit?: () => void;
    onContinue?: (reason?: string) => void;
    onCompleteWithFailures?: () => void;
  }) => (
    <div>
      <span>Rollout status for {rollout.id}</span>
      <span>{isMutating ? `Loading ${rollout.id}` : `Ready ${rollout.id}`}</span>
      <span>
        {announceEvidenceStatus === false ? `Aggregate announces ${rollout.id}` : `Child announces ${rollout.id}`}
      </span>
      {onAdmit && rollout.state === "created" ? (
        <button type="button" onClick={onAdmit}>
          Retry model start for {rollout.id}
        </button>
      ) : null}
      {rollout.batches[rollout.batches.length - 1]?.evidenceSummary?.cumulativeDeltaBasisPoints !== undefined ? (
        <span>{`Cumulative delta ${rollout.batches[rollout.batches.length - 1]?.evidenceSummary?.cumulativeDeltaBasisPoints} basis points`}</span>
      ) : null}
      {onContinue ? (
        <button type="button" onClick={() => onContinue("Override held hashrate evidence")}>
          Continue held rollout
        </button>
      ) : null}
      {onCompleteWithFailures ? (
        <button type="button" onClick={onCompleteWithFailures}>
          Complete with failures
        </button>
      ) : null}
    </div>
  ),
}));

const { default: RolloutLanesTab } = await import("./RolloutLanesTab");

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{`${location.pathname}${location.search}`}</div>;
};

const ClearSetupParamButton = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  return (
    <button
      type="button"
      onClick={() => {
        const nextParams = new URLSearchParams(searchParams);
        nextParams.delete("setupLane");
        setSearchParams(nextParams);
      }}
    >
      Remove setup param
    </button>
  );
};

const TestApp = ({ initialEntry = "/settings/firmware?tab=rolloutLanes" }: { initialEntry?: string }) => (
  <MemoryRouter initialEntries={[initialEntry]}>
    <RolloutLanesTab />
    <ClearSetupParamButton />
    <LocationProbe />
  </MemoryRouter>
);

const renderRolloutLanesTab = (initialEntry?: string) => render(<TestApp initialEntry={initialEntry} />);

function lane(id: string, label: string, rolloutId?: string, currentChannelId = 41n): RolloutLane {
  return {
    id,
    label,
    description: "",
    currentChannelId,
    revision: 1n,
    channels: rolloutId
      ? [
          {
            channelId: 42n,
            releaseSetId: 8n,
            position: 1,
            rolloutId,
            current: currentChannelId === 42n,
          },
        ]
      : [],
    memberCount: 2,
    memberIdentifiers: ["miner-1", "miner-2"],
    currentReleaseTargets: [],
    firmwareConvergence: {
      totalCount: 2,
      pendingCount: 0,
      updatingCount: 0,
      verifyingCount: 0,
      confirmedCount: 2,
      attentionCount: 0,
      members: [],
    },
    models: [],
    scalarProjectionAvailable: true,
    topologyEnabled: false,
  };
}

function rollout(
  id: string,
  state: RolloutRecord["state"],
  memberStates: RolloutMemberState[] = ["succeeded"],
): RolloutRecord {
  return {
    id,
    name: `Rollout ${id}`,
    strategyKey: BETWEEN_CHANNEL_STRATEGY_KEY,
    state,
    revision: 2n,
    sourceChannelId: 41n,
    targetChannelId: 42n,
    reason: "Validated release",
    batches: [
      {
        id: 1n,
        position: 0,
        label: "Final",
        state: state === "review" ? "completed" : "admitted",
        revision: 1n,
        members: [],
      },
    ],
    members: memberStates.map((memberState, index) => ({
      id: BigInt(index + 1),
      batchId: 1n,
      deviceIdentifier: `miner-${index + 1}`,
      position: index,
      state: memberState,
      revision: 1n,
      evidence: [],
    })),
    causes: [],
    availableActions: {
      admit: false,
      continue: false,
      pause: state === "running",
      resume: false,
      abort: state === "running",
      revert: state === "aborted",
      complete: state === "review",
    },
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("RolloutLanesTab", () => {
  beforeEach(() => {
    const storedValues = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      clear: () => storedValues.clear(),
      getItem: (key: string) => storedValues.get(key) ?? null,
      removeItem: (key: string) => storedValues.delete(key),
      setItem: (key: string, value: string) => storedValues.set(key, value),
    });
    vi.useRealTimers();
    vi.clearAllMocks();
    capturedTable.onStart = null;
    capturedTable.onSetup = null;
    capturedTable.onManageMembers = null;
    capturedTable.onView = null;
    capturedTable.onDelete = null;
    rolloutApi.lane = null;
    rolloutApi.lanes = [lane("lane-1", "Stable production")];
    rolloutApi.rollouts = [];
    rolloutApi.isLoading = false;
    rolloutApi.isMutating = false;
    rolloutApi.loadError = null;
    rolloutApi.mutationError = null;
    rolloutApi.topologyReadiness = {
      enabled: false,
      revision: 1n,
      anomalyCount: 0n,
      activeLegacyRolloutCount: 0n,
      anomalies: [],
    };
    rolloutApi.isTopologyReadinessLoading = false;
    rolloutApi.topologyReadinessError = null;
    rolloutApi.topologyReadinessForbidden = false;
    rolloutApi.topologyReadinessStale = false;
    Object.assign(rolloutApi.permissions, {
      canReadChannels: true,
      canManageChannels: false,
      canRead: true,
      canManage: false,
      canControl: false,
    });
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLaneTopologyReadiness.mockResolvedValue(rolloutApi.topologyReadiness);
    rolloutApi.repairRolloutLaneModelBinding.mockResolvedValue(rolloutApi.topologyReadiness);
    rolloutApi.enableRolloutLaneModelTopology.mockResolvedValue({
      ...rolloutApi.topologyReadiness,
      enabled: true,
      revision: 2n,
    });
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.getRolloutLane.mockImplementation(async ({ laneId }: { laneId: string }) => {
      const result = rolloutApi.lanes.find(({ id }) => id === laneId);
      if (!result) {
        throw new Error("Lane not found");
      }
      return result;
    });
    rolloutApi.getRollout.mockImplementation(async ({ rolloutId }: { rolloutId: string }) => {
      const result = rolloutApi.rollouts.find(({ id }) => id === rolloutId);
      if (!result) {
        throw new Error("Rollout not found");
      }
      return result;
    });
    rolloutApi.getRolloutGroup.mockRejectedValue(new Error("Aggregate rollout not found"));
    rolloutApi.completeRollout.mockImplementation(async () => rolloutApi.rollouts[0]);
    rolloutApi.deleteRolloutLane.mockResolvedValue(undefined);
    rolloutApi.listRolloutLaneMembers.mockResolvedValue({ members: [], nextPageToken: "", totalCount: 0 });
    rolloutApi.previewRolloutLaneMembershipChange.mockResolvedValue({
      targetFirmwarePreview: {
        targets: [],
        miners: [],
        matchingCount: 0,
        mismatchedCount: 0,
        unknownCount: 0,
        reassignments: [],
        requiresReassignmentConfirmation: false,
        reassignmentConfirmationToken: "",
      },
      reassignments: [],
      removals: [],
      requiresFirmwareConfirmation: false,
      requiresReassignmentConfirmation: false,
    });
    pushToastMock.mockReset();
    rolloutApi.previewRolloutLane.mockResolvedValue({
      targets: [],
      miners: [],
      matchingCount: 2,
      mismatchedCount: 0,
      unknownCount: 0,
      reassignments: [],
      requiresReassignmentConfirmation: false,
      reassignmentConfirmationToken: "",
    });
    listFirmwareFiles.mockResolvedValue([]);
  });

  it("reconstructs a control-free parent and focused model child from URL IDs", async () => {
    const user = userEvent.setup();
    const child = {
      ...rollout("child-1", "running"),
      parentId: "parent-1",
      laneModelId: "model-1",
      modelIdentityKey: "v1:5:proto:5:alpha",
      manufacturer: "Proto",
      model: "Alpha",
    };
    rolloutApi.lanes = [lane("lane-1", "Stable production", child.id)];
    rolloutApi.rollouts = [child];
    rolloutApi.getRolloutGroup.mockResolvedValue({
      id: "parent-1",
      laneId: "lane-1",
      name: "Proto only",
      reason: "Validated model release",
      children: [child],
    });

    renderRolloutLanesTab("/settings/firmware?tab=rolloutLanes&rolloutParent=parent-1&rolloutChild=child-1");

    expect(await screen.findByTestId("rollout-parent-summary")).toHaveTextContent("Proto only");
    expect(screen.getByTestId("rollout-parent-summary")).toHaveTextContent("Controls are available");
    expect(screen.getByText("Rollout status for child-1")).toBeInTheDocument();
    expect(screen.getByText("Aggregate announces child-1")).toBeInTheDocument();
    expect(rolloutApi.getRolloutGroup).toHaveBeenCalledWith(expect.objectContaining({ parentId: "parent-1" }));

    const trigger = screen.getByRole("button", { name: /Proto Alpha/ });
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(trigger).toHaveAttribute("aria-controls", "rollout-child-child-1");
    expect(document.getElementById("rollout-child-child-1")).toBeInTheDocument();

    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/settings/firmware?tab=rolloutLanes&rolloutParent=parent-1",
    );
    expect(screen.queryByText("Rollout status for child-1")).not.toBeInTheDocument();

    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("location-probe")).toHaveTextContent("rolloutChild=child-1");
  });

  it("shows parent-scoped loading while a URL-selected aggregate is hydrating", async () => {
    const parent = deferred<{
      id: string;
      laneId: string;
      name: string;
      reason: string;
      children: RolloutRecord[];
    }>();
    rolloutApi.getRolloutGroup.mockReturnValue(parent.promise);

    renderRolloutLanesTab("/settings/firmware?tab=rolloutLanes&rolloutParent=parent-loading");

    expect(await screen.findByText("Loading overall rollout...")).toBeInTheDocument();
    expect(screen.getByTestId("rollout-lanes-table")).toBeInTheDocument();

    await act(async () => {
      parent.resolve({
        id: "parent-loading",
        laneId: "lane-1",
        name: "Loaded aggregate",
        reason: "Validated model releases",
        children: [],
      });
      await parent.promise;
    });
    expect(await screen.findByTestId("rollout-parent-summary")).toHaveTextContent("Loaded aggregate");
    expect(screen.queryByText("Loading overall rollout...")).not.toBeInTheDocument();
  });

  it("keeps a successful sibling visible while one child admission fails and retries locally", async () => {
    const user = userEvent.setup();
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canManage = true;
    rolloutApi.permissions.canControl = true;
    const pendingBatch = (child: RolloutRecord): RolloutRecord => ({
      ...child,
      parentId: "parent-admission",
      laneModelId: `model-${child.id}`,
      modelIdentityKey: `identity-${child.id}`,
      batches: [{ ...child.batches[0], state: "pending", admissionAttempt: 0 }],
    });
    const protoChild = {
      ...pendingBatch(rollout("child-proto", "created", ["pending"])),
      manufacturer: "Proto",
      model: "Alpha",
    };
    const antminerChild = {
      ...pendingBatch(rollout("child-antminer", "created", ["pending"])),
      manufacturer: "Antminer",
      model: "S21",
    };
    const parent = {
      id: "parent-admission",
      laneId: "lane-1",
      name: "Independent admission",
      reason: "Update both models",
      resultRevision: 0n,
      terminalOutcome: "pending" as const,
      resultReady: false,
      lifecycle: "active" as const,
      activity: "created" as const,
      needsAction: false,
      evidenceReadiness: "pending" as const,
      models: [],
      children: [protoChild, antminerChild],
    };
    rolloutApi.startRolloutLane.mockResolvedValue({
      lane: rolloutApi.lanes[0],
      rollout: protoChild,
      parent,
      children: [
        { rollout: protoChild, firstBatchId: protoChild.batches[0].id },
        { rollout: antminerChild, firstBatchId: antminerChild.batches[0].id },
      ],
    });
    rolloutApi.getRolloutGroup.mockResolvedValue(parent);
    let antminerAttempts = 0;
    rolloutApi.admitRollout.mockImplementation(async ({ rolloutId }: { rolloutId: string }) => {
      if (rolloutId === antminerChild.id && antminerAttempts++ === 0) {
        throw new Error("Antminer admission outcome could not be confirmed");
      }
      const child = rolloutId === protoChild.id ? protoChild : antminerChild;
      return { ...child, state: "running" as const, revision: child.revision + 1n };
    });

    renderRolloutLanesTab();
    await user.click(await screen.findByRole("button", { name: "Start Stable production" }));
    await user.click(await screen.findByRole("button", { name: "Confirm start Stable production" }));

    expect(await screen.findByTestId("rollout-parent-summary")).toHaveTextContent("Independent admission");
    expect(await screen.findByText(/Antminer admission outcome could not be confirmed/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Proto Alpha.*running/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Antminer S21.*created/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Antminer S21.*created/ }));
    await user.click(screen.getByRole("button", { name: "Retry model start for child-antminer" }));
    await waitFor(() => expect(rolloutApi.admitRollout).toHaveBeenCalledTimes(3));
    expect(screen.queryByText(/Antminer admission outcome could not be confirmed/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Proto Alpha.*running/ })).toBeInTheDocument();
  });

  it("clears aggregate focus when rollout read permission disappears", async () => {
    const child = { ...rollout("child-1", "running"), parentId: "parent-1" };
    rolloutApi.getRolloutGroup.mockResolvedValue({
      id: "parent-1",
      laneId: "lane-1",
      name: "Proto only",
      reason: "Validated model release",
      children: [child],
    });
    const view = renderRolloutLanesTab(
      "/settings/firmware?tab=rolloutLanes&rolloutParent=parent-1&rolloutChild=child-1",
    );
    expect(await screen.findByTestId("rollout-parent-summary")).toBeInTheDocument();

    rolloutApi.permissions.canRead = false;
    view.rerender(<TestApp />);

    await waitFor(() => expect(screen.queryByTestId("rollout-parent-summary")).not.toBeInTheDocument());
  });

  it("shows deterministic initial loading before lane content is available", async () => {
    const pending = deferred<RolloutLane[]>();
    rolloutApi.listRolloutLanes.mockReturnValue(pending.promise);

    renderRolloutLanesTab();
    expect(screen.getByText("Loading rollout lanes...")).toBeInTheDocument();

    await act(async () => {
      pending.resolve(rolloutApi.lanes);
      await pending.promise;
    });
    expect(await screen.findByTestId("rollout-lanes-table")).toBeInTheDocument();
  });

  it("loads history once and refreshes lane pointers after rollout events", async () => {
    const { unmount } = renderRolloutLanesTab();

    await waitFor(() => {
      expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
      expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);
    });
    expect(await screen.findByText("Stable production")).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
    });
    await waitFor(() => expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2));
    expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);

    unmount();
    act(() => {
      window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
    });
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2);
  });

  it("does not load firmware files for rollout managers without channel management", async () => {
    rolloutApi.permissions.canManage = true;

    renderRolloutLanesTab();

    await waitFor(() => expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1));
    expect(listFirmwareFiles).not.toHaveBeenCalled();
  });

  it("loads firmware files for channel managers", async () => {
    rolloutApi.permissions.canManageChannels = true;

    renderRolloutLanesTab();

    await waitFor(() => expect(listFirmwareFiles).toHaveBeenCalledTimes(1));
  });

  it("shows read-only topology readiness without administration controls", async () => {
    renderRolloutLanesTab();

    expect(await screen.findByText("Model topology is ready")).toBeInTheDocument();
    expect(screen.getByText(/Channel management access is required/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Enable model topology" })).not.toBeInTheDocument();
    expect(rolloutApi.getRolloutLaneTopologyReadiness).toHaveBeenCalledOnce();
  });

  it("lets channel managers enable a ready topology and refresh authoritative lanes", async () => {
    const user = userEvent.setup();
    rolloutApi.permissions.canManageChannels = true;

    renderRolloutLanesTab();
    await user.click(await screen.findByRole("button", { name: "Enable model topology" }));

    await waitFor(() =>
      expect(rolloutApi.enableRolloutLaneModelTopology).toHaveBeenCalledWith(
        expect.objectContaining({
          expectedRevision: 1n,
          reason: "Enable rollout lane model topology after readiness review",
        }),
      ),
    );
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2);
  });

  it("offers only U1-supported binding repairs to channel managers", async () => {
    const user = userEvent.setup();
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.topologyReadiness = {
      enabled: false,
      revision: 4n,
      anomalyCount: 1n,
      activeLegacyRolloutCount: 0n,
      anomalies: [
        {
          id: "anomaly-1",
          laneId: "lane-1",
          deviceIdentifier: "miner-1",
          laneModelId: "model-1",
          laneModelRevision: 2n,
          type: "missingBinding",
          supportedRepairActions: ["repairBinding"],
          details: {},
        },
      ],
    };

    renderRolloutLanesTab();
    await user.click(await screen.findByRole("button", { name: "Repair miner-1" }));

    expect(rolloutApi.repairRolloutLaneModelBinding).toHaveBeenCalledWith(
      expect.objectContaining({
        laneId: "lane-1",
        laneModelId: "model-1",
        deviceIdentifier: "miner-1",
        expectedRevision: 2n,
      }),
    );
    expect(screen.getByText("Model topology is not ready")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Enable model topology" })).toBeDisabled();
  });

  it("keeps loaded lanes visible when readiness is stale or forbidden", async () => {
    rolloutApi.topologyReadinessStale = true;
    rolloutApi.topologyReadinessError = "Readiness changed while the repair was applied.";
    const view = renderRolloutLanesTab();

    expect(await screen.findByText("Stable production")).toBeInTheDocument();
    expect(screen.getByText("Topology readiness may be stale")).toBeInTheDocument();
    expect(screen.getByText("Readiness changed while the repair was applied.")).toBeInTheDocument();

    rolloutApi.topologyReadiness = null;
    rolloutApi.topologyReadinessStale = false;
    rolloutApi.topologyReadinessError = "permission denied";
    rolloutApi.topologyReadinessForbidden = true;
    view.rerender(<TestApp />);

    expect(screen.getByText("Stable production")).toBeInTheDocument();
    expect(screen.getByText("Topology readiness is read only")).toBeInTheDocument();
  });

  it("opens lane membership for channel readers without management controls", async () => {
    const user = userEvent.setup();
    renderRolloutLanesTab();

    await user.click(await screen.findByRole("button", { name: "Members Stable production" }));

    expect(screen.getByText("Manage members for Stable production")).toBeInTheDocument();
    expect(screen.getByText("Membership read only")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Complete membership update with transition" }),
    ).not.toBeInTheDocument();
  });

  it("opens live setup after a membership update creates transition members", async () => {
    const user = userEvent.setup();
    rolloutApi.permissions.canManageChannels = true;
    renderRolloutLanesTab();

    await user.click(await screen.findByRole("button", { name: "Members Stable production" }));
    expect(screen.getByText("Membership manager")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Complete membership update with transition" }));

    await waitFor(() =>
      expect(screen.getByTestId("location-probe")).toHaveTextContent(
        "/settings/firmware?tab=rolloutLanes&setupLane=lane-1",
      ),
    );
    expect(screen.queryByText("Manage members for Stable production")).not.toBeInTheDocument();
    expect(screen.getByText("Firmware convergence for Stable production")).toBeInTheDocument();
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
    expect(pushToastMock).toHaveBeenCalledWith({
      message: "Updated Stable production membership",
      status: "success",
    });
  });

  it("keeps membership management open after a pure removal", async () => {
    const user = userEvent.setup();
    rolloutApi.permissions.canManageChannels = true;
    renderRolloutLanesTab();

    await user.click(await screen.findByRole("button", { name: "Members Stable production" }));
    await user.click(screen.getByRole("button", { name: "Complete pure removal" }));

    expect(screen.getByText("Manage members for Stable production")).toBeInTheDocument();
    expect(screen.queryByText("Firmware convergence for Stable production")).not.toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).not.toHaveTextContent("setupLane=");
  });

  it("disables deletion for channel managers without rollout read access", async () => {
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canRead = false;

    renderRolloutLanesTab();

    expect(await screen.findByRole("button", { name: "Delete Stable production" })).toBeDisabled();
    expect(
      screen.getByText("Rollout read access is required to verify this lane is safe to delete."),
    ).toBeInTheDocument();
    expect(rolloutApi.listRollouts).not.toHaveBeenCalled();
  });

  it("polls only active and unsettled rollout IDs while preserving history", async () => {
    vi.useFakeTimers();
    rolloutApi.rollouts = [
      rollout("running", "running", ["admitted"]),
      rollout("aborted-unsettled", "aborted", ["succeeded", "admitted"]),
      rollout("history", "completed"),
    ];
    rolloutApi.lanes = [
      lane("lane-running", "Running", "running"),
      lane("lane-aborted", "Aborted", "aborted-unsettled"),
      lane("lane-history", "History", "history", 42n),
    ];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);

    renderRolloutLanesTab();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(rolloutApi.getRollout.mock.calls.map(([request]) => request.rolloutId).sort()).toEqual([
      "aborted-unsettled",
      "running",
    ]);
    expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2);
    expect(rolloutApi.completeRollout).not.toHaveBeenCalled();
  });

  it("polls each aggregate parent once and merges its children", async () => {
    vi.useFakeTimers();
    const first = { ...rollout("child-a", "running", ["admitted"]), parentId: "parent-1" };
    const second = { ...rollout("child-b", "review", ["succeeded"]), parentId: "parent-1" };
    rolloutApi.rollouts = [first, second];
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.getRolloutGroup.mockResolvedValue({
      id: "parent-1",
      laneId: "lane-1",
      name: "Two models",
      reason: "test",
      children: [first, second],
    });

    renderRolloutLanesTab();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(rolloutApi.getRolloutGroup).toHaveBeenCalledOnce();
    expect(rolloutApi.getRolloutGroup).toHaveBeenCalledWith(expect.objectContaining({ parentId: "parent-1" }));
    expect(rolloutApi.getRollout).not.toHaveBeenCalled();
  });

  it("polls a completed result until final evidence refreshes in place", async () => {
    vi.useFakeTimers();
    const openResult: RolloutRecord = {
      ...rollout("completed", "completed"),
      batches: [
        {
          ...rollout("completed", "completed").batches[0],
          state: "completed",
          completedAt: "2026-08-18T02:00:00.000Z",
          evidenceSummary: {
            status: "observing",
            totalCount: 2n,
            pairedCount: 2n,
            cumulativeDeltaBasisPoints: -100,
            postWindowFinalized: false,
          },
        },
      ],
    };
    rolloutApi.rollouts = [openResult];
    rolloutApi.lanes = [lane("lane-completed", "Completed", openResult.id, 42n)];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);

    const view = renderRolloutLanesTab("/settings/firmware?tab=rolloutLanes&rollout=completed");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByText("Cumulative delta -100 basis points")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(1);

    rolloutApi.rollouts = [
      {
        ...openResult,
        batches: [
          {
            ...openResult.batches[0],
            evidenceSummary: {
              ...openResult.batches[0].evidenceSummary!,
              status: "finalized",
              cumulativeDeltaBasisPoints: -250,
              postWindowFinalized: true,
            },
          },
        ],
      },
    ];
    view.rerender(<TestApp initialEntry="/settings/firmware?tab=rolloutLanes&rollout=completed" />);
    expect(screen.getByText("Cumulative delta -250 basis points")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(1);
  });

  it("does not poll a legacy completed batch without a completion time or evidence summary", async () => {
    vi.useFakeTimers();
    const legacyResult = rollout("legacy-completed", "completed");
    rolloutApi.rollouts = [legacyResult];
    rolloutApi.lanes = [lane("lane-legacy", "Legacy completed", legacyResult.id, 42n)];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);

    renderRolloutLanesTab();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(rolloutApi.getRollout).not.toHaveBeenCalled();
  });

  it("polls lane status while firmware convergence is active", async () => {
    vi.useFakeTimers();
    rolloutApi.lanes = [
      {
        ...lane("lane-initial", "Initial convergence"),
        firmwareConvergence: {
          totalCount: 2,
          pendingCount: 1,
          updatingCount: 1,
          verifyingCount: 0,
          confirmedCount: 0,
          attentionCount: 0,
          members: [],
        },
      },
    ];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);

    renderRolloutLanesTab();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Firmware convergence for Initial convergence")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close setup" })).not.toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/settings/firmware?tab=rolloutLanes");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
    expect(rolloutApi.getRolloutLane).toHaveBeenCalledWith(
      expect.objectContaining({
        laneId: "lane-initial",
        includeDeviceSetMembers: false,
        includeFirmwareConvergenceMembers: true,
      }),
    );
    expect(rolloutApi.getRollout).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(25_000);
    });
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2);
  });

  it("refreshes the lane table on rollout events during active setup", async () => {
    const activeLane = {
      ...lane("lane-initial", "Initial convergence"),
      firmwareConvergence: {
        totalCount: 2,
        pendingCount: 1,
        updatingCount: 1,
        verifyingCount: 0,
        confirmedCount: 0,
        attentionCount: 0,
        members: [],
      },
    };
    const otherLane = lane("lane-other", "Created by another operator");
    rolloutApi.lanes = [activeLane];
    rolloutApi.listRolloutLanes.mockResolvedValueOnce(rolloutApi.lanes).mockImplementationOnce(async () => {
      rolloutApi.lanes = [activeLane, otherLane];
      return rolloutApi.lanes;
    });

    const view = renderRolloutLanesTab();
    await waitFor(() => expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1));

    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await waitFor(() => expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2));
    view.rerender(<TestApp />);

    expect(screen.getByText("Created by another operator")).toBeInTheDocument();
  });

  it("keeps an active setup visible when setupLane is manually removed", async () => {
    const user = userEvent.setup();
    const activeLane = {
      ...lane("lane-initial", "Initial convergence"),
      firmwareConvergence: {
        totalCount: 2,
        pendingCount: 1,
        updatingCount: 1,
        verifyingCount: 0,
        confirmedCount: 0,
        attentionCount: 0,
        members: [],
      },
    };
    rolloutApi.lane = activeLane;
    rolloutApi.lanes = [activeLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(activeLane);

    renderRolloutLanesTab("/settings/firmware?tab=rolloutLanes&setupLane=lane-initial");
    expect(await screen.findByText("Firmware convergence for Initial convergence")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close setup" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Remove setup param" }));

    expect(screen.getByTestId("location-probe")).toHaveTextContent("/settings/firmware?tab=rolloutLanes");
    expect(screen.getByText("Firmware convergence for Initial convergence")).toBeInTheDocument();
  });

  it("does not overlap bounded rollout refreshes", async () => {
    vi.useFakeTimers();
    const liveRollout = rollout("running", "running", ["admitted"]);
    const firstRefresh = deferred<RolloutRecord>();
    rolloutApi.rollouts = [liveRollout];
    rolloutApi.lanes = [lane("lane-running", "Running", liveRollout.id)];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.getRollout.mockReturnValueOnce(firstRefresh.promise).mockResolvedValue(liveRollout);

    renderRolloutLanesTab();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(1);

    await act(async () => {
      firstRefresh.resolve(liveRollout);
      await firstRefresh.promise;
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(2);
  });

  it("closes creation and focuses the new lane setup view", async () => {
    const user = userEvent.setup();
    const created = {
      ...lane("lane-new", "New stable lane"),
      firmwareConvergence: {
        totalCount: 1,
        pendingCount: 1,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 0,
        attentionCount: 0,
        members: [
          {
            deviceIdentifier: "miner-1",
            manufacturer: "Proto",
            model: "Alpha",
            targetFirmwareVersion: "2.0.0",
            state: "pending" as const,
            updatedAt: timestampFromDate(new Date("2026-08-18T12:00:00.000Z")),
          },
        ],
      },
    };
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.createRolloutLane.mockImplementation(async () => {
      rolloutApi.lane = created;
      return created;
    });

    renderRolloutLanesTab("/settings/firmware?site=alpha");
    await user.click(await screen.findByRole("button", { name: "Create lane" }));
    await user.click(screen.getByRole("button", { name: "Submit lane creation" }));

    expect(await screen.findByText("Firmware convergence for New stable lane")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Submit lane creation" })).not.toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-new",
    );
    expect(rolloutApi.getRolloutLane).not.toHaveBeenCalledWith(expect.objectContaining({ laneId: created.id }));
  });

  it("uses the committed declaration mutation response without refetching the lane", async () => {
    const user = userEvent.setup();
    const selectedLane = {
      ...lane("lane-model", "Model lane"),
      topologyEnabled: true,
      scalarProjectionAvailable: false,
    };
    rolloutApi.lanes = [selectedLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);

    renderRolloutLanesTab();
    await user.click(await screen.findByRole("button", { name: "Declarations Model lane" }));
    rolloutApi.getRolloutLane.mockClear();
    await user.click(screen.getByRole("button", { name: "Complete declaration mutation" }));

    expect(rolloutApi.getRolloutLane).not.toHaveBeenCalled();
    expect(pushToastMock).toHaveBeenCalledWith({
      message: "Updated Model lane declarations",
      status: "success",
    });
  });

  it("restores URL-selected setup details immediately after remount", async () => {
    const selectedLane = {
      ...lane("lane-selected", "Selected lane"),
      firmwareConvergence: {
        totalCount: 1,
        pendingCount: 1,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 0,
        attentionCount: 0,
        members: [],
      },
    };
    rolloutApi.lane = selectedLane;
    rolloutApi.lanes = [selectedLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(selectedLane);
    const initialEntry = "/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-selected";

    const firstView = renderRolloutLanesTab(initialEntry);
    await waitFor(() =>
      expect(rolloutApi.getRolloutLane).toHaveBeenCalledWith(
        expect.objectContaining({
          laneId: selectedLane.id,
          includeDeviceSetMembers: false,
          includeFirmwareConvergenceMembers: true,
        }),
      ),
    );
    expect(screen.getByText("Firmware convergence for Selected lane")).toBeInTheDocument();

    firstView.unmount();
    rolloutApi.getRolloutLane.mockClear();
    renderRolloutLanesTab(initialEntry);

    await waitFor(() =>
      expect(rolloutApi.getRolloutLane).toHaveBeenCalledWith(
        expect.objectContaining({
          laneId: selectedLane.id,
          includeDeviceSetMembers: false,
          includeFirmwareConvergenceMembers: true,
        }),
      ),
    );
    expect(screen.getByText("Firmware convergence for Selected lane")).toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(initialEntry);
  });

  it("restores the exact URL-selected rollout result and acknowledges it after remount", async () => {
    const active = rollout("active", "running", ["admitted"]);
    const completed = rollout("completed", "completed");
    rolloutApi.rollouts = [active, completed];
    rolloutApi.lanes = [
      lane("lane-active", "Active lane", active.id),
      lane("lane-completed", "Completed lane", completed.id, 42n),
    ];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    const initialEntry = "/settings/firmware?tab=rolloutLanes&rollout=completed";

    const firstView = renderRolloutLanesTab(initialEntry);
    expect(await screen.findByText("Rollout status for completed")).toBeInTheDocument();
    expect(screen.queryByText("Rollout status for active")).not.toBeInTheDocument();
    await waitFor(() => expect(localStorage.getItem("protoFleet.acknowledgedRolloutResultId")).toBe('"completed"'));

    firstView.unmount();
    renderRolloutLanesTab(initialEntry);
    expect(await screen.findByText("Rollout status for completed")).toBeInTheDocument();
  });

  it("does not crash on an invalid URL-selected rollout", async () => {
    rolloutApi.getRollout.mockRejectedValue(new Error("Rollout not found"));

    renderRolloutLanesTab("/settings/firmware?tab=rolloutLanes&rollout=not-a-rollout");

    await waitFor(() =>
      expect(rolloutApi.getRollout).toHaveBeenCalledWith(expect.objectContaining({ rolloutId: "not-a-rollout" })),
    );
    expect(screen.getByTestId("rollout-lanes-table")).toBeInTheDocument();
    expect(localStorage.getItem("protoFleet.acknowledgedRolloutResultId")).toBeNull();
  });

  it("does not load or acknowledge a URL-selected rollout without rollout read permission", async () => {
    const completed = rollout("completed", "completed");
    rolloutApi.rollouts = [completed];
    rolloutApi.lanes = [lane("lane-completed", "Completed lane", completed.id, 42n)];
    rolloutApi.permissions.canRead = false;

    renderRolloutLanesTab("/settings/firmware?tab=rolloutLanes&rollout=completed");
    await waitFor(() => expect(rolloutApi.listRolloutLanes).toHaveBeenCalledOnce());

    expect(rolloutApi.getRollout).not.toHaveBeenCalled();
    expect(screen.queryByText("Rollout status for completed")).not.toBeInTheDocument();
    expect(localStorage.getItem("protoFleet.acknowledgedRolloutResultId")).toBeNull();
  });

  it("closing setup removes only setupLane from the URL", async () => {
    const selectedLane = lane("lane-selected", "Selected lane");
    rolloutApi.lane = selectedLane;
    rolloutApi.lanes = [selectedLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(selectedLane);

    renderRolloutLanesTab("/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-selected");
    await userEvent.click(await screen.findByRole("button", { name: "Close setup" }));

    expect(screen.getByTestId("location-probe")).toHaveTextContent("/settings/firmware?site=alpha&tab=rolloutLanes");
  });

  it("successful deletion clears focused setup without an unconditional list refresh", async () => {
    const user = userEvent.setup();
    const selectedLane = lane("lane-selected", "Selected lane");
    rolloutApi.lane = selectedLane;
    rolloutApi.lanes = [selectedLane];
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(selectedLane);

    renderRolloutLanesTab("/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-selected");
    await user.click(await screen.findByRole("button", { name: "Delete Selected lane" }));
    await user.click(screen.getByRole("button", { name: "Confirm delete lane" }));

    await waitFor(() =>
      expect(rolloutApi.deleteRolloutLane).toHaveBeenCalledWith(
        expect.objectContaining({
          laneId: selectedLane.id,
          expectedRevision: selectedLane.revision,
          reason: "Delete rollout lane",
        }),
      ),
    );
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
    expect(pushToastMock).toHaveBeenCalledWith({
      message: "Deleted Selected lane",
      status: "success",
    });
    expect(screen.queryByText("Delete dialog for Selected lane")).not.toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/settings/firmware?site=alpha&tab=rolloutLanes");
  });

  it("derives the delete key from the freshest lane revision", async () => {
    const user = userEvent.setup();
    const selectedLane = lane("lane-selected", "Selected lane");
    rolloutApi.lane = selectedLane;
    rolloutApi.lanes = [selectedLane];
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(selectedLane);
    rolloutApi.deleteRolloutLane.mockRejectedValue(new Error("Rollout work is still active."));
    const initialEntry = "/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-selected";

    const view = renderRolloutLanesTab(initialEntry);
    await user.click(await screen.findByRole("button", { name: "Delete Selected lane" }));
    const refreshedLane = {
      ...selectedLane,
      label: "Updated selected lane",
      revision: 2n,
    };
    rolloutApi.lane = refreshedLane;
    rolloutApi.lanes = [refreshedLane];
    view.rerender(<TestApp initialEntry={initialEntry} />);
    expect(screen.getByText("Delete dialog for Updated selected lane")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm delete lane" }));
    await waitFor(() => expect(rolloutApi.deleteRolloutLane).toHaveBeenCalledTimes(1));
    expect(rolloutApi.deleteRolloutLane.mock.calls[0][0].expectedRevision).toBe(2n);

    rolloutApi.mutationError = "Rollout work is still active.";
    const retriedLane = {
      ...refreshedLane,
      revision: 3n,
    };
    rolloutApi.lane = retriedLane;
    rolloutApi.lanes = [retriedLane];
    view.rerender(<TestApp initialEntry={initialEntry} />);
    await user.click(screen.getByRole("button", { name: "Confirm delete lane" }));

    await waitFor(() => expect(rolloutApi.deleteRolloutLane).toHaveBeenCalledTimes(2));
    const firstKey = rolloutApi.deleteRolloutLane.mock.calls[0][0].idempotencyKey;
    const retryKey = rolloutApi.deleteRolloutLane.mock.calls[1][0].idempotencyKey;
    expect(firstKey).toBe(`delete-lane:${selectedLane.id}:2`);
    expect(retryKey).toBe(`delete-lane:${selectedLane.id}:3`);
    expect(rolloutApi.deleteRolloutLane.mock.calls[1][0].expectedRevision).toBe(3n);
    expect(screen.getByText("Rollout work is still active.")).toBeInTheDocument();
    expect(pushToastMock).not.toHaveBeenCalled();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(initialEntry);
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
  });

  it("replays a lost successful deletion after reopening and removes the stale lane", async () => {
    const user = userEvent.setup();
    const selectedLane = lane("lane-selected", "Selected lane");
    rolloutApi.lanes = [selectedLane];
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.deleteRolloutLane
      .mockRejectedValueOnce(new Error("Response lost after archive"))
      .mockImplementationOnce(async () => {
        rolloutApi.lanes = [];
      });

    renderRolloutLanesTab();
    await user.click(await screen.findByRole("button", { name: "Delete Selected lane" }));
    await user.click(screen.getByRole("button", { name: "Confirm delete lane" }));
    await waitFor(() => expect(rolloutApi.deleteRolloutLane).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole("button", { name: "Cancel delete lane" }));

    await user.click(screen.getByRole("button", { name: "Delete Selected lane" }));
    await user.click(screen.getByRole("button", { name: "Confirm delete lane" }));

    await waitFor(() => expect(rolloutApi.deleteRolloutLane).toHaveBeenCalledTimes(2));
    const firstRequest = rolloutApi.deleteRolloutLane.mock.calls[0][0];
    const replayRequest = rolloutApi.deleteRolloutLane.mock.calls[1][0];
    expect(firstRequest.idempotencyKey).toBe(`delete-lane:${selectedLane.id}:${selectedLane.revision}`);
    expect(replayRequest).toMatchObject({
      expectedRevision: firstRequest.expectedRevision,
      idempotencyKey: firstRequest.idempotencyKey,
      reason: firstRequest.reason,
    });
    await waitFor(() => expect(screen.queryByText("Selected lane")).not.toBeInTheDocument());
    expect(pushToastMock).toHaveBeenCalledWith({
      message: "Deleted Selected lane",
      status: "success",
    });
  });

  it("keeps normal load error handling for an invalid URL-selected lane", async () => {
    rolloutApi.lanes = [];
    rolloutApi.listRolloutLanes.mockResolvedValue([]);
    rolloutApi.getRolloutLane.mockRejectedValue(new Error("Lane not found"));
    const initialEntry = "/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=missing-lane";

    renderRolloutLanesTab(initialEntry);

    expect(await screen.findByText("Firmware rollout lanes are unavailable")).toBeInTheDocument();
    expect(screen.getByText("Lane not found")).toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(initialEntry);
  });

  it("retries selected setup detail hydration with the aggregate load", async () => {
    const user = userEvent.setup();
    const activeLane = {
      ...lane("lane-initial", "Initial convergence"),
      firmwareConvergence: {
        totalCount: 1,
        pendingCount: 1,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 0,
        attentionCount: 0,
        members: [],
      },
    };
    rolloutApi.lane = activeLane;
    rolloutApi.lanes = [activeLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockRejectedValueOnce(new Error("Detail hydration failed")).mockResolvedValue(activeLane);

    renderRolloutLanesTab();

    expect(await screen.findByText("Detail hydration failed")).toBeInTheDocument();
    expect(rolloutApi.getRolloutLane).toHaveBeenCalledTimes(1);
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(rolloutApi.getRolloutLane).toHaveBeenCalledTimes(2));
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2);
  });

  it("successful rollout start removes setupLane from the URL", async () => {
    const user = userEvent.setup();
    const selectedLane = lane("lane-selected", "Selected lane");
    const startedRollout = { ...rollout("started", "running", ["admitted"]), batches: [] };
    const startRequest = deferred<{ rollout: RolloutRecord }>();
    rolloutApi.lane = selectedLane;
    rolloutApi.lanes = [selectedLane];
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canManage = true;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(selectedLane);
    rolloutApi.startRolloutLane.mockReturnValue(startRequest.promise);

    renderRolloutLanesTab("/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-selected");
    await user.click(await screen.findByRole("button", { name: "Start ready lane" }));
    await user.click(await screen.findByRole("button", { name: "Confirm start Selected lane" }));

    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-selected",
    );
    await act(async () => {
      startRequest.resolve({ rollout: startedRollout });
      await startRequest.promise;
    });
    await waitFor(() =>
      expect(screen.getByTestId("location-probe")).toHaveTextContent("/settings/firmware?site=alpha&tab=rolloutLanes"),
    );
  });

  it("reopens setup from a lane row and polls details without full membership", async () => {
    vi.useFakeTimers();
    const activeLane = {
      ...lane("lane-initial", "Initial convergence"),
      firmwareConvergence: {
        totalCount: 2,
        pendingCount: 1,
        updatingCount: 0,
        verifyingCount: 1,
        confirmedCount: 0,
        attentionCount: 0,
        members: [
          {
            deviceIdentifier: "miner-1",
            manufacturer: "Proto",
            model: "Alpha",
            targetFirmwareVersion: "2.0.0",
            state: "pending" as const,
            updatedAt: timestampFromDate(new Date("2026-08-18T12:00:00.000Z")),
          },
          {
            deviceIdentifier: "miner-2",
            manufacturer: "Proto",
            model: "Alpha",
            targetFirmwareVersion: "2.0.0",
            state: "verifying" as const,
            updatedAt: timestampFromDate(new Date("2026-08-18T12:00:05.000Z")),
          },
        ],
      },
    };
    rolloutApi.lanes = [activeLane];
    rolloutApi.lane = activeLane;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(activeLane);

    renderRolloutLanesTab("/settings/firmware?site=alpha");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    await act(async () => {
      screen.getByRole("button", { name: "Setup Initial convergence" }).click();
      await Promise.resolve();
    });
    expect(screen.getByText("Firmware convergence for Initial convergence")).toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-initial",
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(rolloutApi.getRolloutLane).toHaveBeenCalledWith(
      expect.objectContaining({
        laneId: activeLane.id,
        includeDeviceSetMembers: false,
        includeFirmwareConvergenceMembers: true,
        firmwareConvergenceMembersUpdatedAfter: timestampFromDate(new Date("2026-08-18T12:00:05.000Z")),
      }),
    );
    expect(rolloutApi.getRolloutLane).not.toHaveBeenCalledWith(
      expect.objectContaining({
        includeDeviceSetMembers: true,
      }),
    );
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
  });

  it("finishes detail hydration when aggregate readiness arrives first", async () => {
    const liveRollout = rollout("running", "running", ["admitted"]);
    const setupLane = {
      ...lane("lane-setup", "Setup lane"),
      firmwareConvergence: {
        totalCount: 1,
        pendingCount: 1,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 0,
        attentionCount: 0,
        members: [
          {
            deviceIdentifier: "miner-1",
            manufacturer: "Proto",
            model: "Alpha",
            targetFirmwareVersion: "2.0.0",
            state: "pending" as const,
            updatedAt: timestampFromDate(new Date("2026-08-18T12:00:00.000Z")),
          },
        ],
      },
    };
    const runningLane = lane("lane-running", "Running lane", liveRollout.id);
    rolloutApi.lane = setupLane;
    rolloutApi.lanes = [setupLane, runningLane];
    rolloutApi.rollouts = [liveRollout];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.getRolloutLane.mockResolvedValue(setupLane);

    const view = renderRolloutLanesTab();
    await waitFor(() => expect(capturedTable.onSetup).not.toBeNull());
    act(() => capturedTable.onSetup?.(setupLane));
    await act(async () => await Promise.resolve());
    expect(screen.getByTestId("setup-member-states")).toHaveTextContent("miner-1:pending");

    const detailResponse = deferred<RolloutLane>();
    let detailSignal: AbortSignal | undefined;
    rolloutApi.getRolloutLane.mockClear();
    rolloutApi.getRolloutLane.mockImplementation(({ signal }: { signal?: AbortSignal }) => {
      detailSignal = signal;
      return detailResponse.promise;
    });

    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await waitFor(() => expect(rolloutApi.getRolloutLane).toHaveBeenCalledTimes(1));
    expect(rolloutApi.getRollout).toHaveBeenCalledWith(expect.objectContaining({ rolloutId: liveRollout.id }));

    const aggregateReady = {
      ...setupLane,
      firmwareConvergence: {
        ...setupLane.firmwareConvergence,
        pendingCount: 0,
        confirmedCount: 1,
        members: [],
      },
    };
    rolloutApi.lanes = [aggregateReady, runningLane];
    view.rerender(<TestApp />);
    expect(detailSignal?.aborted).toBe(false);
    expect(screen.getByTestId("setup-member-states")).toHaveTextContent("miner-1:pending");

    const detailedReady = {
      ...aggregateReady,
      firmwareConvergence: {
        ...aggregateReady.firmwareConvergence,
        members: [{ ...setupLane.firmwareConvergence.members[0], state: "confirmed" as const }],
      },
    };
    await act(async () => {
      rolloutApi.lane = detailedReady;
      detailResponse.resolve(detailedReady);
      await detailResponse.promise;
      view.rerender(<TestApp />);
    });

    expect(screen.getByTestId("setup-member-states")).toHaveTextContent("miner-1:confirmed");
  });

  it("keeps polling focused needs-attention setup details", async () => {
    vi.useFakeTimers();
    const attentionLane = {
      ...lane("lane-attention", "Attention lane"),
      firmwareConvergence: {
        totalCount: 2,
        pendingCount: 0,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 1,
        attentionCount: 1,
        members: [],
      },
    };
    rolloutApi.lanes = [attentionLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.getRolloutLane.mockResolvedValue(attentionLane);

    renderRolloutLanesTab();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    await act(async () => {
      screen.getByRole("button", { name: "Setup Attention lane" }).click();
      await Promise.resolve();
    });
    rolloutApi.getRolloutLane.mockClear();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(rolloutApi.getRolloutLane).toHaveBeenCalledWith(
      expect.objectContaining({
        laneId: attentionLane.id,
        includeDeviceSetMembers: false,
        includeFirmwareConvergenceMembers: true,
      }),
    );
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
  });

  it("keeps the lane table visible during background and preparation loads", async () => {
    const user = userEvent.setup();
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canManage = true;
    rolloutApi.isLoading = true;
    const preparation = deferred<RolloutLane>();
    rolloutApi.getRolloutLane.mockReturnValue(preparation.promise);

    renderRolloutLanesTab();
    expect(await screen.findByTestId("rollout-lanes-table")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Start Stable production" }));
    expect(screen.getByTestId("rollout-lanes-table")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start Stable production" })).toBeDisabled();
  });

  it("ignores an out-of-order lane preparation response", async () => {
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canManage = true;
    const firstLane = lane("lane-1", "First");
    const secondLane = lane("lane-2", "Second");
    rolloutApi.lanes = [firstLane, secondLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    const first = deferred<RolloutLane>();
    const signals: AbortSignal[] = [];
    rolloutApi.getRolloutLane.mockImplementation(({ laneId, signal }: { laneId: string; signal?: AbortSignal }) => {
      if (signal) {
        signals.push(signal);
      }
      return laneId === firstLane.id ? first.promise : Promise.resolve(secondLane);
    });

    renderRolloutLanesTab();
    await waitFor(() => expect(capturedTable.onStart).not.toBeNull());
    act(() => {
      capturedTable.onStart?.(firstLane);
      capturedTable.onStart?.(secondLane);
    });

    expect(await screen.findByText("Prepared Second")).toBeInTheDocument();
    expect(signals[0]?.aborted).toBe(true);

    await act(async () => {
      first.resolve(firstLane);
      await first.promise;
    });
    expect(screen.queryByText("Prepared First")).not.toBeInTheDocument();
  });

  it("loads full membership only when preparing a ready lane rollout", async () => {
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canManage = true;

    renderRolloutLanesTab();
    await waitFor(() => expect(capturedTable.onStart).not.toBeNull());
    act(() => {
      capturedTable.onStart?.(rolloutApi.lanes[0]);
    });

    await waitFor(() =>
      expect(rolloutApi.getRolloutLane).toHaveBeenCalledWith(
        expect.objectContaining({
          laneId: rolloutApi.lanes[0].id,
          includeDeviceSetMembers: true,
          includeFirmwareConvergenceMembers: false,
        }),
      ),
    );
  });

  it("completes a settled failed review with explicit approval", async () => {
    const user = userEvent.setup();
    const failedReview = rollout("failed-review", "review", ["succeeded", "failed"]);
    rolloutApi.rollouts = [failedReview];
    rolloutApi.lanes = [lane("lane-1", "Stable production", failedReview.id)];
    rolloutApi.permissions.canControl = true;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.completeRollout.mockResolvedValue({
      ...failedReview,
      state: "completedWithFailures",
      revision: 3n,
    });

    renderRolloutLanesTab();
    await user.click(await screen.findByRole("button", { name: "Complete with failures" }));

    expect(rolloutApi.completeRollout).toHaveBeenCalledWith(
      expect.objectContaining({
        rolloutId: failedReview.id,
        expectedRevision: failedReview.revision,
        withFailures: true,
      }),
    );
  });

  it("confirms a held override from the rollout detail modal with evidence context", async () => {
    const user = userEvent.setup();
    const baseHeldReview = rollout("held-review", "review");
    const heldReview = {
      ...baseHeldReview,
      hashratePolicy: {
        maxDropBasisPoints: 10,
        healthyDurationSeconds: 30,
      },
      batches: [
        {
          ...baseHeldReview.batches[0],
          label: "Pilot",
          evidenceSummary: {
            status: "held" as const,
            totalCount: 3n,
            pairedCount: 3n,
            latestPolicyBucketDeltaBasisPoints: -400,
            postWindowFinalized: false,
          },
        },
        {
          ...baseHeldReview.batches[0],
          id: 2n,
          position: 1,
          label: "Remaining",
          state: "pending" as const,
        },
      ],
      availableActions: {
        ...baseHeldReview.availableActions,
        continue: true,
      },
    };
    rolloutApi.rollouts = [heldReview];
    rolloutApi.lanes = [lane("lane-1", "Stable production", heldReview.id)];
    rolloutApi.permissions.canControl = true;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.continueRollout.mockResolvedValue({
      ...heldReview,
      state: "running",
      revision: heldReview.revision + 1n,
    });

    renderRolloutLanesTab();
    await user.click(await screen.findByRole("button", { name: `View ${heldReview.name}` }));
    const detailModal = await screen.findByTestId("view-rollout-modal");

    await user.click(within(detailModal).getByRole("button", { name: "Continue" }));
    expect(rolloutApi.continueRollout).not.toHaveBeenCalled();
    expect(screen.getByText("Configured maximum drop: 0.10%")).toBeInTheDocument();
    expect(screen.getByText("Latest policy bucket: −4.00%")).toBeInTheDocument();
    expect(screen.getByText("Paired coverage: 3 of 3")).toBeInTheDocument();
    expect(screen.getByText(/admits the next batch despite this held evidence/i)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(rolloutApi.continueRollout).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Continue anyway" })).not.toBeInTheDocument();

    await user.click(within(detailModal).getByRole("button", { name: "Continue" }));
    await user.click(screen.getByRole("button", { name: "Continue anyway" }));

    expect(rolloutApi.continueRollout).toHaveBeenCalledOnce();
    expect(rolloutApi.continueRollout).toHaveBeenCalledWith(
      expect.objectContaining({
        rolloutId: heldReview.id,
        expectedRevision: heldReview.revision,
        reason: "Override held hashrate evidence",
      }),
    );
  });
});
