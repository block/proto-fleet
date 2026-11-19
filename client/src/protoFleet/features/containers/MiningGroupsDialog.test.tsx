import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import MiningGroupsDialog from "./MiningGroupsDialog";
import { GetStateResponseSchema } from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";

const mocks = vi.hoisted(() => ({
  permissions: [] as string[],
  getState: vi.fn(),
  activate: vi.fn(),
  deactivate: vi.fn(),
  selector: vi.fn(),
  refresh: vi.fn(),
  returnDefault: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({ miningGroupsClient: mocks }));
vi.mock("@/protoFleet/features/poolConfigurations/assignmentProgress", () => ({
  refreshPoolAssignments: mocks.refresh,
}));
vi.mock("@/protoFleet/store", () => ({ usePermissions: () => mocks.permissions }));
vi.mock("./ReturnHashboardsToDefaultDialog", () => ({
  default: (props: { deviceIdentifier: string; onComplete: () => void }) => {
    mocks.returnDefault(props);
    return (
      <div role="dialog" aria-label="Return hashboards to Default">
        <button onClick={props.onComplete}>Done</button>
      </div>
    );
  },
}));
vi.mock("@/protoFleet/features/poolConfigurations/ConfigurationSelector", () => ({
  ConfigurationSelector: (props: unknown) => {
    mocks.selector(props);
    return <div>Shared pool configuration picker</div>;
  },
}));
beforeEach(() => {
  vi.resetAllMocks();
  mocks.permissions = [
    "miner:update_pools",
    "pool:read",
    "miner:activate_mining_groups",
    "miner:deactivate_mining_groups",
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
});
afterEach(() => vi.restoreAllMocks());

it("keeps disable blocked and offers the Default move until unallocated boards are moved", async () => {
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      status: "idle",
      observedAt: timestampFromDate(new Date()),
      groups: [{ id: "cleared", macAddresses: ["02:00:00:00:00:09"] }],
      canReturnToDefault: true,
      deactivationBlockedReason: "Move hashboards out of non-default mining groups before disabling mining groups.",
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Move unallocated hashboards" }));
  expect(mocks.deactivate).not.toHaveBeenCalled();
  expect(mocks.returnDefault).toHaveBeenLastCalledWith(expect.objectContaining({ deviceIdentifier: "ct1" }));
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      status: "complete",
      observedAt: timestampFromDate(new Date()),
      groups: [{ id: "default", macAddresses: ["02:00:00:00:00:09"] }],
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Disable mining groups" })).toBeEnabled());
  expect(mocks.deactivate).not.toHaveBeenCalled();
});

it.each([
  "Recall all allocated hashboards before disabling mining groups.",
  "Wait for pending pool changes and ownership cleanup to finish before disabling mining groups.",
  "Wait for pending device changes or resolve uncertain writes before disabling mining groups.",
])("explains why deactivation is blocked: %s", async (reason) => {
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      status: "idle",
      observedAt: timestampFromDate(new Date()),
      deactivationBlockedReason: reason,
      groups: [{ id: "client-group", macAddresses: ["02:00:00:00:00:09"] }],
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={() => {}} />);
  expect(await screen.findByText(reason)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Disable mining groups" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Move unallocated hashboards" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(screen.queryByText(/The container will keep/)).not.toBeInTheDocument();
  expect(mocks.deactivate).not.toHaveBeenCalled();
});

it("submits deactivation and waits for confirmed disabled mode", async () => {
  mocks.deactivate.mockResolvedValue({ operationId: "deactivation" });
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={() => {}} />);
  const disable = await screen.findByRole("button", { name: "Disable mining groups" });
  await waitFor(() => expect(disable).toBeEnabled());
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: false,
      supported: true,
      status: "complete",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  fireEvent.click(disable);
  expect(await screen.findByText("Mining groups disabled.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
  expect(mocks.deactivate).toHaveBeenCalledWith(
    expect.objectContaining({ deviceIdentifier: "ct1", acknowledgeInterruption: true, requestId: expect.any(String) }),
  );
  expect(mocks.activate).not.toHaveBeenCalled();
});

it.each(["pending", "applying", "blocked", "complete"])(
  "shows deactivation %s without a pre-submission blocker",
  async (status) => {
    const reason = "Wait for the mining-group change or recovery to finish before disabling mining groups.";
    mocks.getState.mockResolvedValue(
      create(GetStateResponseSchema, {
        enabled: status !== "complete",
        supported: true,
        status,
        message: "Waiting for controller confirmation",
        observedAt: timestampFromDate(new Date()),
        deactivationBlockedReason: reason,
      }),
    );
    render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={() => {}} />);
    const message = status === "complete" ? "Mining groups disabled." : "Waiting for controller confirmation";
    expect(await screen.findByText(message)).toBeVisible();
    expect(screen.queryByText(reason)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: status === "complete" ? "Done" : "Close" })).toBeEnabled();
    expect(mocks.deactivate).not.toHaveBeenCalled();
  },
);

it("resumes a blocked deactivation with its saved operation ID", async () => {
  const onClose = vi.fn();
  let finish: (() => void) | undefined;
  mocks.deactivate.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      status: "blocked",
      operationId: "saved-deactivation",
      canResumeDeactivation: true,
      message: "Operator access changed. Resume disabling mining groups to continue.",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={onClose} />);
  fireEvent.click(await screen.findByRole("button", { name: "Resume disabling" }));
  expect(mocks.deactivate).toHaveBeenCalledExactlyOnceWith({
    deviceIdentifier: "ct1",
    requestId: "saved-deactivation",
    acknowledgeInterruption: true,
  });
  expect(screen.getByRole("button", { name: "Submitting…" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();
  expect(onClose).not.toHaveBeenCalled();
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      status: "applying",
      operationId: "saved-deactivation",
      message: "Disabling mining groups…",
      observedAt: timestampFromDate(new Date()),
    }),
  );
  await act(async () => finish?.());
  expect(screen.queryByRole("button", { name: "Resume disabling" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(mocks.getState).toHaveBeenCalledTimes(2);
});

it("offers resume after this dialog submitted the paused deactivation", async () => {
  mocks.deactivate.mockImplementationOnce(({ requestId }: { requestId: string }) => {
    mocks.getState.mockResolvedValue(
      create(GetStateResponseSchema, {
        enabled: true,
        supported: true,
        status: "blocked",
        operationId: requestId,
        canResumeDeactivation: true,
        observedAt: timestampFromDate(new Date()),
      }),
    );
    return Promise.resolve({ operationId: requestId });
  });
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={vi.fn()} />);
  const disable = await screen.findByRole("button", { name: "Disable mining groups" });
  await waitFor(() => expect(disable).toBeEnabled());
  fireEvent.click(disable);
  fireEvent.click(await screen.findByRole("button", { name: "Resume disabling" }));
  await waitFor(() => expect(mocks.deactivate).toHaveBeenCalledTimes(2));
  expect(mocks.deactivate.mock.calls[1][0].requestId).toEqual(mocks.deactivate.mock.calls[0][0].requestId);
});

it.each([
  { name: "without the server flag", canResume: false, fresh: true, missing: "", operationId: "saved" },
  { name: "with stale state", canResume: true, fresh: false, missing: "", operationId: "saved" },
  { name: "without an operation ID", canResume: true, fresh: true, missing: "", operationId: "" },
  {
    name: "without disable permission",
    canResume: true,
    fresh: true,
    missing: "miner:deactivate_mining_groups",
    operationId: "saved",
  },
  {
    name: "without start permission",
    canResume: true,
    fresh: true,
    missing: "miner:start_mining",
    operationId: "saved",
  },
  { name: "without stop permission", canResume: true, fresh: true, missing: "miner:stop_mining", operationId: "saved" },
])("does not resume deactivation $name", async ({ canResume, fresh, missing, operationId }) => {
  mocks.permissions = mocks.permissions.filter((permission) => permission !== missing);
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      status: "blocked",
      operationId,
      canResumeDeactivation: canResume,
      message: "Recovery is paused",
      observedAt: fresh ? timestampFromDate(new Date()) : undefined,
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={vi.fn()} />);
  await screen.findByText("Recovery is paused");
  expect(screen.queryByRole("button", { name: "Resume disabling" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(mocks.deactivate).not.toHaveBeenCalled();
});

it("keeps stale state, access, and read errors visible during deactivation", async () => {
  const intervals = vi.spyOn(window, "setInterval");
  mocks.permissions = mocks.permissions.filter((permission) => permission !== "miner:deactivate_mining_groups");
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      enabled: true,
      supported: true,
      status: "blocked",
      message: "Recovery is paused",
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={() => {}} />);
  await screen.findByText("Recovery is paused");
  expect(screen.getByText(/Disabling mining groups requires permission/)).toBeVisible();
  mocks.permissions.push("miner:deactivate_mining_groups");
  mocks.getState.mockRejectedValue(new Error("Connection lost"));
  await act(async () => {
    for (const [callback, delay] of intervals.mock.calls) {
      if (delay === 4000 && typeof callback === "function") callback();
    }
  });
  expect(screen.getByText("Connection lost")).toBeVisible();
  expect(
    screen.getByText("Controller configuration is stale. Reconnect the controller before making changes."),
  ).toBeVisible();
  expect(screen.getByText("Recovery is paused")).toBeVisible();
});

it("keeps the deactivation request identity after a lost reply", async () => {
  mocks.deactivate
    .mockRejectedValueOnce(new Error("Connection lost"))
    .mockResolvedValueOnce({ operationId: "deactivation" });
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode="deactivate" onClose={() => {}} />);
  const disable = await screen.findByRole("button", { name: "Disable mining groups" });
  await waitFor(() => expect(disable).toBeEnabled());
  fireEvent.click(disable);
  await screen.findByText("Connection lost");
  fireEvent.click(disable);
  await waitFor(() => expect(mocks.deactivate).toHaveBeenCalledTimes(2));
  expect(mocks.deactivate.mock.calls[1][0].requestId).toEqual(mocks.deactivate.mock.calls[0][0].requestId);
});

it.each([
  { mode: "activate" as const, enabled: false, label: "Enable mining groups" },
  { mode: "deactivate" as const, enabled: true, label: "Disable mining groups" },
])("prevents $label across repeated stale display snapshots", async ({ mode, enabled, label }) => {
  const intervals = vi.spyOn(window, "setInterval");
  const snapshot = create(GetStateResponseSchema, {
    supported: true,
    enabled,
    status: "idle",
    observedAt: timestampFromDate(new Date()),
  });
  mocks.getState.mockResolvedValue(snapshot);
  render(<MiningGroupsDialog deviceIdentifier="ct1" mode={mode} onClose={() => {}} />);
  const action = await screen.findByRole("button", { name: label });
  await waitFor(() => expect(action).toBeEnabled());

  // Failed controller reads retain display content, but revoke its freshness.
  mocks.getState.mockResolvedValue({ ...snapshot, observedAt: undefined });
  for (let read = 2; read <= 3; read++) {
    await act(async () => {
      for (const [callback, delay] of intervals.mock.calls) {
        if (delay === 4000 && typeof callback === "function") callback();
      }
    });
    expect(mocks.getState).toHaveBeenCalledTimes(read);
    await waitFor(() =>
      expect(
        screen.getByText("Controller configuration is stale. Reconnect the controller before making changes."),
      ).toBeVisible(),
    );
    expect(screen.queryByText("Loading controller configuration…")).not.toBeInTheDocument();
    if (mode === "deactivate") {
      expect(screen.queryByRole("button", { name: label })).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
    } else {
      expect(action).toBeDisabled();
      fireEvent.click(action);
    }
  }
  expect(mocks.activate).not.toHaveBeenCalled();
  expect(mocks.deactivate).not.toHaveBeenCalled();

  mocks.getState.mockResolvedValue(snapshot);
  await act(async () => {
    for (const [callback, delay] of intervals.mock.calls) {
      if (delay === 4000 && typeof callback === "function") callback();
    }
  });
  expect(action).toBeEnabled();
});

it.each([{ moduleIds: [42n] }, { hashboardIds: [101n, 103n] }])(
  "delegates board pool assignment to the shared configuration picker: %s",
  async (boards) => {
    const onClose = vi.fn();
    render(<MiningGroupsDialog deviceIdentifier="ct1" {...boards} onClose={onClose} />);
    expect(screen.getByText("Shared pool configuration picker")).toBeInTheDocument();
    expect(mocks.selector).toHaveBeenCalledWith(expect.objectContaining({ boards, onDismiss: onClose }));
    expect(mocks.getState).not.toHaveBeenCalled();
    expect(mocks.activate).not.toHaveBeenCalled();
  },
);

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
      "Fleet will stop mining before it enables mining groups. Fleet preserves pool intent for every identified hashboard. Mapped boards can receive pool changes immediately. Assignments for boards with unresolved locations stay pending until their locations are confirmed. When ready, choose Wake default mining group from the Fleet container menu.",
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
      message: "Mining groups enabled. Choose Wake default mining group from the Fleet container menu.",
      groups: [{ id: "default", mining: false }],
      observedAt: timestampFromDate(new Date()),
      revision: 2n,
    }),
  );
  await act(async () => {
    for (const [callback, delay] of intervals.mock.calls) {
      if (delay === 4000 && typeof callback === "function") callback();
    }
  });
  await screen.findByText("Mining groups enabled. Choose Wake default mining group from the Fleet container menu.");
  expect(screen.queryByText("Choose Wake default mining group from the Fleet container menu.")).not.toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(mocks.activate).toHaveBeenCalledOnce();
});

it("shows retained pool assignment guidance after activation completes", async () => {
  const message =
    "Mining groups are enabled. The container pool assignment is retained until all hashboard locations are confirmed. Pool changes are blocked.";
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      supported: true,
      enabled: true,
      status: "complete",
      message,
      observedAt: timestampFromDate(new Date()),
    }),
  );
  render(<MiningGroupsDialog deviceIdentifier="ct1" onClose={vi.fn()} />);
  await screen.findByText(message);
  expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
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
