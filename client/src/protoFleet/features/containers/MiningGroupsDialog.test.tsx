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
  selector: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({ miningGroupsClient: mocks }));
vi.mock("@/protoFleet/store", () => ({ usePermissions: () => mocks.permissions }));
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
