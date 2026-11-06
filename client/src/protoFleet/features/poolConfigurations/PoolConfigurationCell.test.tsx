import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { type Client, Code, ConnectError } from "@connectrpc/connect";
import { PoolAssignmentsContext } from "./assignmentContext";
import { trackPoolOperation, usePoolAssignmentProgress } from "./assignmentProgress";
import { BoardPoolConfigurationCell } from "./BoardPoolConfigurationCell";
import { PoolConfigurationCell } from "./PoolConfigurationCell";
import {
  ConfigurationSchema,
  EquipmentAssignmentSchema,
  type GetOperationResponse,
  GetOperationResponseSchema,
  OperationTargetSchema,
  type PoolConfigurationService,
  type ReviewedEquipment,
  ReviewedEquipmentSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

let controller: AbortController;
beforeEach(() => {
  vi.useFakeTimers();
  controller = new AbortController();
  usePoolAssignmentProgress.setState({ jobs: [], detailsJobId: undefined, detailsSelection: undefined });
});
afterEach(async () => {
  controller.abort();
  await vi.runOnlyPendingTimersAsync();
  vi.useRealTimers();
});

const start = (
  state?: string,
  equipment: ReviewedEquipment[] = [
    create(ReviewedEquipmentSchema, { kind: "device", targetId: 7n, deviceIdentifier: "miner-a" }),
  ],
) => {
  const getOperation = vi.fn(() =>
    state === undefined
      ? new Promise<GetOperationResponse>((resolve) => {
          controller.signal.addEventListener("abort", () => resolve(create(GetOperationResponseSchema)));
        })
      : state === "unavailable"
        ? Promise.reject(new ConnectError("status unavailable", Code.NotFound))
        : Promise.resolve(
            create(GetOperationResponseSchema, {
              operationId: "operation",
              targets: equipment.map(({ kind, targetId, deviceIdentifier, label }) =>
                create(OperationTargetSchema, { kind, targetId, deviceIdentifier, label, state }),
              ),
            }),
          ),
  );
  return trackPoolOperation(
    { getOperation } as unknown as Client<typeof PoolConfigurationService>,
    "",
    create(ConfigurationSchema, { configurationId: "config", name: "Production" }),
    "operation",
    equipment,
    controller.signal,
  );
};

describe("accepted pool operation progress in miner rows", () => {
  it("shows updating before the first operation response", () => {
    act(() => {
      void start();
    });
    render(<PoolConfigurationCell deviceIdentifier="miner-a" />);
    expect(screen.getByText("Updating…")).toBeVisible();
    expect(screen.getByTestId("pool-configuration-progress")).toHaveClass("animate-spin");
    expect(usePoolAssignmentProgress.getState().jobs[0].operationId).toBe("operation");
  });

  it.each([
    ["pending", "Updating…", true],
    ["applying", "Updating…", true],
    ["verified", "Production", false],
    ["failed", "Update failed", false],
    ["unavailable", "Unavailable", false],
  ] as const)("shows accepted operation state %s as %s", async (state, label, spinning) => {
    await act(async () => {
      void start(state);
    });
    render(<PoolConfigurationCell deviceIdentifier="miner-a" />);
    expect(screen.getByText(label)).toBeVisible();
    if (spinning) {
      expect(screen.getByTestId("pool-configuration-progress")).toHaveClass("animate-spin");
      expect(screen.queryByRole("button")).not.toBeInTheDocument();
    } else {
      expect(screen.queryByTestId("pool-configuration-progress")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: label })).toHaveClass("hover:underline");
      expect(screen.getByRole("button", { name: label })).not.toHaveClass("underline");
    }
  });
});

it("replaces unavailable operation progress with fresh server evidence", async () => {
  await act(async () => {
    void start("unavailable");
  });
  const assignment = create(EquipmentAssignmentSchema, {
    deviceId: 7n,
    deviceIdentifier: "miner-a",
    name: "New assignment",
    state: "verified",
  });
  const view = (loadedAt: number) => (
    <PoolAssignmentsContext.Provider
      value={{
        byIdentifier: new Map([["miner-a", assignment]]),
        byId: new Map([["7", assignment]]),
        readable: true,
        loadedAt,
        loading: false,
        error: null,
        refresh: () => {},
      }}
    >
      <PoolConfigurationCell deviceIdentifier="miner-a" />
    </PoolAssignmentsContext.Provider>
  );
  const rendered = render(view(Date.now() - 1));
  expect(screen.getByText("Unavailable")).toBeVisible();
  rendered.rerender(view(Date.now() + 1));
  expect(screen.getByText("New assignment")).toBeVisible();
  expect(screen.getByRole("button", { name: "New assignment" })).toBeVisible();
});

it("maps an accepted board operation to its module and hashboard until fresh assignments arrive", async () => {
  const equipment = [create(ReviewedEquipmentSchema, { kind: "hashboard_installation", targetId: 101n })];
  await act(async () => {
    void start("pending", equipment);
  });
  const board = create(EquipmentAssignmentSchema, {
    installationId: 101n,
    hashboardId: 11n,
    moduleId: 1n,
    deviceId: 10n,
    configurationId: "config",
    name: "Production",
    state: "verified",
  });
  const view = (loadedAt: number) => (
    <PoolAssignmentsContext.Provider
      value={{
        byIdentifier: new Map(),
        byId: new Map(),
        boards: [board],
        readable: true,
        loadedAt,
        loading: false,
        error: null,
        refresh: () => {},
      }}
    >
      <BoardPoolConfigurationCell moduleId={1n} />
      <BoardPoolConfigurationCell hashboardId={11n} />
    </PoolAssignmentsContext.Provider>
  );
  const rendered = render(view(Date.now() - 1));
  expect(screen.getAllByText("Updating…")).toHaveLength(2);
  rendered.rerender(view(Date.now() + 1));
  expect(screen.getAllByText("Production")).toHaveLength(2);
  expect(screen.queryByTestId("pool-configuration-progress")).not.toBeInTheDocument();
});
