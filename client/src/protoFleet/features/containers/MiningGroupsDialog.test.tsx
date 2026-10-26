import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import MiningGroupsDialog from "./MiningGroupsDialog";
import {
  type GetStateResponse,
  GetStateResponseSchema,
} from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";
const mocks = vi.hoisted(() => ({
  permissions: [] as string[],
  getState: vi.fn(),
  resolveFleetPools: vi.fn(),
  getModuleHashboards: vi.fn(),
  setPools: vi.fn(),
  activate: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({ miningGroupsClient: mocks, fleetInventoryClient: mocks }));
vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({
    miningPools: [
      { poolId: "1", name: "Client pools", poolUrl: "stratum+tcp://pool.example:3333", username: "worker" },
    ],
    validatePool: vi.fn(),
  }),
}));
vi.mock("@/protoFleet/api/useMinerPoolAssignments", () => ({
  default: () => ({ fetchPoolAssignments: vi.fn(), isLoading: false }),
}));
vi.mock("@/protoFleet/store", () => ({
  usePermissions: () => mocks.permissions,
}));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));
beforeEach(() => {
  vi.resetAllMocks();
  mocks.permissions = [
    "miner:update_pools",
    "pool:read",
    "miner:activate_mining_groups",
    "miner:start_mining",
    "miner:stop_mining",
  ];
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      revision: 7n,
      status: "idle",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  mocks.resolveFleetPools.mockResolvedValue({ revision: { revisionId: "profile" } });
  mocks.getModuleHashboards.mockResolvedValue({
    modules: [
      {
        moduleId: 1n,
        slots: [
          { occupancy: "occupied", board: { hashboardId: 101n } },
          { occupancy: "empty" },
          { occupancy: "occupied", board: { hashboardId: 103n } },
        ],
      },
    ],
  });
  mocks.setPools.mockResolvedValue({ operationId: "op" });
});
afterEach(() => vi.restoreAllMocks());
it("expands module selection to the exact existing boards", async () => {
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      moduleIds={[1n]}
      onClose={() => {}}
    />,
  );
  await screen.findByText(/2 hashboards selected/);
  fireEvent.click(screen.getByRole("button", { name: "Add pool" }));
  fireEvent.click(await screen.findByTestId("pool-row-Client pools"));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
  await waitFor(() => expect(apply).toBeEnabled());
  fireEvent.click(apply);
  await waitFor(() =>
    expect(mocks.setPools).toHaveBeenCalledWith(
      expect.objectContaining({
        deviceIdentifier: "ct1",
        hashboardIds: [101n, 103n],
        poolRevisionId: "profile",
        expectedRevision: 7n,
        requestId: expect.any(String),
        userUsername: "operator",
        userPassword: "password",
      }),
    ),
  );
});
it("requires activation and never submits from stale observations", async () => {
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: false,
      supported: true,
      revision: 1n,
      status: "idle",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await screen.findByText(/administrator must enable mining groups/);
  expect(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ })).toBeDisabled();
  expect(mocks.setPools).not.toHaveBeenCalled();
});
it("shows async progress without claiming that hashing has started", async () => {
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      revision: 1n,
      status: "applying",
      message: "Waiting for board shutdown confirmation (1 boards blocked)",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await screen.findByText(/Waiting for board shutdown confirmation/);
  expect(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ })).toBeDisabled();
});

async function selectPool() {
  fireEvent.click(screen.getByRole("button", { name: "Add pool" }));
  fireEvent.click(await screen.findByTestId("pool-row-Client pools"));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
}

it.each([
  { code: Code.FailedPrecondition, reset: true },
  { code: Code.Aborted, reset: true },
  { code: Code.Unavailable, reset: false },
  { code: Code.DeadlineExceeded, reset: false },
  { code: Code.Unknown, reset: false },
])("handles retry identity after submission fails with code $code", async ({ code, reset }) => {
  const intervals = vi.spyOn(globalThis, "setInterval");
  mocks.setPools.mockRejectedValue(new ConnectError("Submission failed", code));
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await selectPool();
  const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
  await waitFor(() => expect(apply).toBeEnabled());
  fireEvent.click(apply);
  await screen.findByRole("alert");
  await waitFor(() => expect(apply).toBeEnabled());
  const first = mocks.setPools.mock.calls[0][0];
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      revision: 8n,
      status: "idle",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  await act(async () => {
    for (const [callback, delay] of intervals.mock.calls) {
      if (delay === 4000 && typeof callback === "function") callback();
    }
  });
  fireEvent.click(apply);
  await waitFor(() => expect(mocks.setPools).toHaveBeenCalledTimes(2));
  const retry = mocks.setPools.mock.calls[1][0];
  expect(retry.expectedRevision).toBe(reset ? 8n : 7n);
  if (reset) expect(retry.requestId).not.toBe(first.requestId);
  else expect(retry).toEqual(first);
});

it.each([false, true])("ignores an older poll after submission (poll fails=%s)", async (fails) => {
  const intervals = vi.spyOn(globalThis, "setInterval");
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await selectPool();
  const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
  await waitFor(() => expect(apply).toBeEnabled());
  let resolvePoll!: (state: GetStateResponse) => void;
  let rejectPoll!: (error: Error) => void;
  mocks.getState.mockReturnValueOnce(
    new Promise((resolve, reject) => {
      resolvePoll = resolve;
      rejectPoll = reject;
    }),
  );
  await act(async () => {
    for (const [callback, delay] of intervals.mock.calls) {
      if (delay === 4000 && typeof callback === "function") callback();
    }
  });
  const pollSignal = mocks.getState.mock.calls[1][1].signal as AbortSignal;
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      revision: 8n,
      status: "applying",
      message: "Moving selected boards",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  fireEvent.click(apply);
  await screen.findByText("Moving selected boards");
  await act(async () => {
    if (fails) rejectPoll(new Error("Old poll failed"));
    else
      resolvePoll(
        create(GetStateResponseSchema, {
          enabled: true,
          supported: true,
          revision: 7n,
          status: "idle",
          observedAt: timestampFromDate(new Date()),
        }),
      );
  });
  expect(screen.getByText("Moving selected boards")).toBeInTheDocument();
  expect(screen.queryByText("Old poll failed")).not.toBeInTheDocument();
  expect(pollSignal.aborted).toBe(true);
  expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
});

it("keeps a successful submission accepted when its refresh is rejected", async () => {
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await selectPool();
  const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
  await waitFor(() => expect(apply).toBeEnabled());
  mocks.getState.mockRejectedValueOnce(new ConnectError("Refresh failed", Code.FailedPrecondition));
  fireEvent.click(apply);
  await screen.findByText("Refresh failed");
  expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
  expect(mocks.setPools).toHaveBeenCalledTimes(1);
});

it("disables submission until the current module target resolves", async () => {
  const view = render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      moduleIds={[1n]}
      onClose={() => {}}
    />,
  );
  await screen.findByText(/2 hashboards selected/);
  await selectPool();
  await waitFor(() => expect(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ })).toBeEnabled());

  type Inventory = {
    modules: { moduleId: bigint; slots: { occupancy: string; board?: { hashboardId: bigint } }[] }[];
  };
  let resolveInventory!: (inventory: Inventory) => void;
  mocks.getModuleHashboards.mockReturnValueOnce(
    new Promise<Inventory>((resolve) => {
      resolveInventory = resolve;
    }),
  );
  view.rerender(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      moduleIds={[2n]}
      onClose={() => {}}
    />,
  );
  expect(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ })).toBeDisabled();
  expect(await screen.findByText("Loading hashboards…")).toBeInTheDocument();
  expect(mocks.setPools).not.toHaveBeenCalled();

  await act(async () => {
    resolveInventory({
      modules: [
        {
          moduleId: 2n,
          slots: [
            { occupancy: "occupied", board: { hashboardId: 201n } },
            { occupancy: "empty" },
            { occupancy: "empty" },
          ],
        },
      ],
    });
  });
  await selectPool();
  await waitFor(() => expect(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ }));
  await waitFor(() => expect(mocks.setPools).toHaveBeenCalledWith(expect.objectContaining({ hashboardIds: [201n] })));
});

it.each([
  { change: "hashboard", deviceIdentifier: "ct1", hashboardIds: [201n] },
  { change: "container", deviceIdentifier: "ct2", hashboardIds: [101n, 103n] },
])("resets the request when the $change changes, but preserves same-target retries", async (nextTarget) => {
  mocks.setPools.mockRejectedValue(new Error("Connection lost"));
  const view = render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n, 103n]}
      onClose={() => {}}
    />,
  );
  await selectPool();
  const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
  await waitFor(() => expect(apply).toBeEnabled());
  fireEvent.click(apply);
  await screen.findByRole("alert");
  await waitFor(() => expect(apply).toBeEnabled());
  const firstRequest = mocks.setPools.mock.calls[0][0];

  // Reordering the same board set is an ordinary refresh, not a new target.
  view.rerender(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[103n, 101n]}
      onClose={() => {}}
    />,
  );
  fireEvent.click(apply);
  await waitFor(() => expect(mocks.setPools).toHaveBeenCalledTimes(2));
  expect(mocks.setPools.mock.calls[1][0]).toEqual(firstRequest);
  await waitFor(() => expect(apply).toBeEnabled());

  view.rerender(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier={nextTarget.deviceIdentifier}
      hashboardIds={nextTarget.hashboardIds}
      onClose={() => {}}
    />,
  );
  expect(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ })).toBeDisabled();
  await selectPool();
  await waitFor(() => expect(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: /Assign to \d+ hashboards?/ }));
  await waitFor(() => expect(mocks.setPools).toHaveBeenCalledTimes(3));
  expect(mocks.setPools.mock.calls[2][0]).toEqual(
    expect.objectContaining({
      deviceIdentifier: nextTarget.deviceIdentifier,
      hashboardIds: nextTarget.hashboardIds,
    }),
  );
  expect(mocks.setPools.mock.calls[2][0].requestId).not.toBe(firstRequest.requestId);
});

it("does not submit an old target after its saved-pool resolution finishes", async () => {
  let resolveProfile!: (value: { revision: { revisionId: string } }) => void;
  mocks.resolveFleetPools.mockReturnValueOnce(
    new Promise((resolve) => {
      resolveProfile = resolve;
    }),
  );
  const view = render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await selectPool();
  fireEvent.click(screen.getByRole("button", { name: "Assign to 1 hashboard" }));
  await waitFor(() => expect(mocks.resolveFleetPools).toHaveBeenCalledOnce());
  view.rerender(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[201n]}
      onClose={() => {}}
    />,
  );
  await act(async () => resolveProfile({ revision: { revisionId: "old-profile" } }));
  expect(mocks.setPools).not.toHaveBeenCalled();
});

it("retains the resolved immutable profile when retrying an uncertain submission", async () => {
  mocks.setPools.mockRejectedValueOnce(new ConnectError("Connection lost", Code.Unavailable));
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await selectPool();
  const assign = screen.getByRole("button", { name: "Assign to 1 hashboard" });
  fireEvent.click(assign);
  await screen.findByRole("alert");
  await waitFor(() => expect(assign).toBeEnabled());
  mocks.resolveFleetPools.mockResolvedValue({ revision: { revisionId: "changed-saved-pool" } });
  fireEvent.click(assign);
  await waitFor(() => expect(mocks.setPools).toHaveBeenCalledTimes(2));
  expect(mocks.resolveFleetPools).toHaveBeenCalledExactlyOnceWith({ poolIds: [1n] });
  expect(mocks.setPools.mock.calls[1][0]).toEqual(mocks.setPools.mock.calls[0][0]);
});

it("shows activation progress and only says Done after completion", async () => {
  const intervals = vi.spyOn(window, "setInterval");
  const onClose = vi.fn();
  mocks.activate.mockResolvedValue({ operationId: "activation" });
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      supported: true,
      enabled: false,
      observedAt: timestampFromDate(new Date()),
      revision: 1n,
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" onClose={onClose} />);
  expect(
    screen.getByText(
      "Fleet will pause mining to enable mining groups, then resume with the existing pools if this container was running. A stopped container will stay stopped.",
    ),
  ).toBeInTheDocument();
  const enable = screen.getByRole("button", { name: "Enable mining groups" });
  await waitFor(() => expect(enable).toBeEnabled());
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      supported: true,
      enabled: false,
      status: "blocked",
      message: "Waiting for mining to stop. Activation will retry automatically.",
      observedAt: timestampFromDate(new Date()),
      revision: 1n,
    }),
  );
  fireEvent.click(enable);
  await screen.findByText("Waiting for mining to stop. Activation will retry automatically.");
  expect(screen.getByRole("alert")).toHaveTextContent("Waiting for mining to stop.");
  expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(screen.queryByText(/run-in reservations/)).not.toBeInTheDocument();
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      supported: true,
      enabled: true,
      status: "complete",
      message: "Configuration applied",
      observedAt: timestampFromDate(new Date()),
      revision: 2n,
    }),
  );
  await act(async () => {
    for (const [callback, delay] of intervals.mock.calls) {
      if (delay === 4000 && typeof callback === "function") callback();
    }
  });
  await screen.findByText("Mining groups enabled.");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(mocks.activate).toHaveBeenCalledOnce();
});

it("can close a pending activation without resubmitting it", async () => {
  const onClose = vi.fn();
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      supported: true,
      enabled: true,
      status: "applying",
      message: "Verifying controller configuration",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" onClose={onClose} />);
  await screen.findByText("Verifying controller configuration");
  expect(screen.queryByText("Mining groups enabled.")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(mocks.activate).not.toHaveBeenCalled();
});

it("submits pool assignments without secure-context randomUUID", async () => {
  vi.stubGlobal("crypto", { getRandomValues: crypto.getRandomValues.bind(crypto) });
  try {
    render(
      <MiningGroupsDialog
        credentials={{ username: "operator", password: "password" }}
        deviceIdentifier="ct1"
        hashboardIds={[101n]}
        onClose={() => {}}
      />,
    );
    await selectPool();
    const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
    await waitFor(() => expect(apply).toBeEnabled());
    fireEvent.click(apply);
    await waitFor(() => expect(mocks.setPools).toHaveBeenCalledTimes(1));
    expect(mocks.setPools.mock.calls[0][0].requestId).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(await screen.findByRole("button", { name: "Done" })).toBeEnabled();
  } finally {
    vi.unstubAllGlobals();
  }
});

it("allows retry after request identity creation fails", async () => {
  vi.spyOn(crypto, "getRandomValues").mockImplementationOnce(() => {
    throw new Error("Randomness unavailable");
  });
  render(
    <MiningGroupsDialog
      credentials={{ username: "operator", password: "password" }}
      deviceIdentifier="ct1"
      hashboardIds={[101n]}
      onClose={() => {}}
    />,
  );
  await selectPool();
  const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
  await waitFor(() => expect(apply).toBeEnabled());
  fireEvent.click(apply);
  await screen.findByText("Randomness unavailable");
  expect(mocks.setPools).not.toHaveBeenCalled();
  expect(apply).toBeEnabled();
  fireEvent.click(apply);
  await waitFor(() => expect(mocks.setPools).toHaveBeenCalledTimes(1));
});

it.each(["miner:start_mining", "miner:stop_mining"])("blocks pool assignment after losing %s", async (missing) => {
  const props = {
    credentials: { username: "operator", password: "password" },
    deviceIdentifier: "ct1",
    hashboardIds: [101n],
    onClose: () => {},
  };
  const view = render(<MiningGroupsDialog {...props} />);
  await selectPool();
  const apply = screen.getByRole("button", { name: /Assign to \d+ hashboards?/ });
  await waitFor(() => expect(apply).toBeEnabled());
  mocks.permissions = mocks.permissions.filter((permission) => permission !== missing);
  view.rerender(<MiningGroupsDialog {...props} />);
  expect(apply).toBeDisabled();
  expect(screen.getByText(/start and stop mining/)).toBeInTheDocument();
  fireEvent.click(apply);
  expect(mocks.setPools).not.toHaveBeenCalled();
});

it.each(["miner:start_mining", "miner:stop_mining"])("blocks activation without %s", async (missing) => {
  mocks.permissions = mocks.permissions.filter((permission) => permission !== missing);
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      supported: true,
      enabled: false,
      status: "idle",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" onClose={() => {}} />);
  await screen.findByText(/start and stop mining/);
  const activate = screen.getByRole("button", { name: "Enable mining groups" });
  expect(activate).toBeDisabled();
  fireEvent.click(activate);
  expect(mocks.activate).not.toHaveBeenCalled();
});

it("shows an interrupted activation as failed even when groups were already enabled", async () => {
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      supported: true,
      enabled: true,
      status: "failed",
      message: "Operator access changed. Review the controller's mining state before continuing.",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" onClose={() => {}} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Operator access changed.");
  expect(screen.queryByText("Mining groups enabled.")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(mocks.activate).not.toHaveBeenCalled();
});
