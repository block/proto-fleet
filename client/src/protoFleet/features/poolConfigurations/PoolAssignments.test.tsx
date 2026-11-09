import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import type { Client } from "@connectrpc/connect";

import { useSavedPoolAssignments } from "./assignmentContext";
import { refreshPoolAssignments, trackPoolOperation, usePoolAssignmentProgress } from "./assignmentProgress";
import { BoardPoolConfigurationCell } from "./BoardPoolConfigurationCell";
import { PoolAssignments } from "./PoolAssignments";
import { PoolConfigurationCell } from "./PoolConfigurationCell";
import {
  CheckConfigurationResponseSchema,
  ConfigurationSchema,
  GetAssignmentsResponseSchema,
  GetOperationResponseSchema,
  type PoolConfigurationService,
  ReviewedEquipmentSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

const mocks = vi.hoisted(() => ({ getAssignments: vi.fn(), readDevicePools: vi.fn(), accountId: "", read: true }));
vi.mock("./api", () => ({
  usePoolConfigurationClient: () => ({ client: mocks, accountId: mocks.accountId }),
  usePoolConfigurationPermissions: () => ({ read: mocks.read, change: true }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.accountId = "";
  mocks.read = true;
  usePoolAssignmentProgress.setState({ jobs: [], assignmentsRevision: 0 });
});

const page = () => (
  <PoolAssignments deviceIdentifiers={["miner-a", "miner-b"]}>
    <PoolConfigurationCell deviceIdentifier="miner-a" />
    <PoolConfigurationCell deviceIdentifier="miner-b" />
  </PoolAssignments>
);
const response = () =>
  create(GetAssignmentsResponseSchema, {
    selectedCount: 2n,
    assignments: [
      { deviceId: 1n, deviceIdentifier: "miner-a", configurationId: "config-a", name: "Production", state: "verified" },
      { deviceId: 2n, deviceIdentifier: "miner-b", configurationId: "config-b", name: "Backup", state: "pending" },
    ],
  });

describe("saved pool assignments", () => {
  it("does not imply empty settings when read permission is missing", () => {
    mocks.read = false;
    render(
      <PoolAssignments deviceIdentifiers={["miner-a"]} moduleIds={[1n]}>
        <PoolConfigurationCell deviceIdentifier="miner-a" />
        <BoardPoolConfigurationCell moduleId={1n} />
      </PoolAssignments>,
    );
    expect(screen.getAllByText("Unavailable")).toHaveLength(2);
    expect(screen.queryByText("—")).not.toBeInTheDocument();
    expect(mocks.getAssignments).not.toHaveBeenCalled();
    expect(mocks.readDevicePools).not.toHaveBeenCalled();
  });

  it("loads a table page in one request and survives remount without local progress", async () => {
    mocks.getAssignments.mockResolvedValue(response());
    const view = render(page());
    expect(await screen.findByText("Production")).toBeVisible();
    expect(screen.getByRole("button", { name: "Production" })).toBeVisible();
    expect(screen.getByText("Updating…")).toBeVisible();
    expect(screen.getByTestId("pool-configuration-progress")).toHaveClass("animate-spin");
    expect(mocks.getAssignments).toHaveBeenCalledTimes(1);
    view.unmount();
    render(page());
    expect(await screen.findByText("Production")).toBeVisible();
    expect(mocks.getAssignments).toHaveBeenCalledTimes(2);
  });

  it("does not let completed session progress resurrect a retired assignment", async () => {
    const now = vi.spyOn(Date, "now").mockReturnValue(1000);
    await trackPoolOperation(
      {
        getOperation: vi.fn().mockResolvedValue(
          create(GetOperationResponseSchema, {
            operationId: "old-operation",
            targets: [{ kind: "device", targetId: 1n, state: "verified" }],
          }),
        ),
      } as unknown as Client<typeof PoolConfigurationService>,
      "",
      create(ConfigurationSchema, { name: "Retired", configurationId: "retired" }),
      "old-operation",
      [create(ReviewedEquipmentSchema, { kind: "device", targetId: 1n, deviceIdentifier: "miner-a" })],
    );
    now.mockRestore();
    mocks.getAssignments.mockResolvedValue(create(GetAssignmentsResponseSchema, { selectedCount: 2n }));
    render(page());
    await waitFor(() => expect(screen.queryByText("Retired")).not.toBeInTheDocument());
    expect(screen.getAllByText("Unavailable")).toHaveLength(2);
  });

  it("clears old account data immediately and ignores late responses", async () => {
    let resolveOld!: (value: ReturnType<typeof response>) => void;
    mocks.getAssignments.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    );
    const view = render(page());
    mocks.accountId = "other-account";
    mocks.getAssignments.mockResolvedValue(create(GetAssignmentsResponseSchema, { selectedCount: 2n }));
    view.rerender(page());
    await waitFor(() => expect(screen.getAllByText("Unavailable")).toHaveLength(2));
    await act(async () => {
      resolveOld(response());
    });
    expect(screen.queryByText("Production")).not.toBeInTheDocument();
  });
});

it("shows mixed module assignments without losing individual board status", async () => {
  mocks.getAssignments.mockResolvedValue(
    create(GetAssignmentsResponseSchema, {
      selectedCount: 2n,
      assignments: [
        {
          installationId: 101n,
          hashboardId: 11n,
          moduleId: 1n,
          deviceId: 10n,
          configurationId: "production",
          name: "Production",
          state: "applying",
        },
        { installationId: 102n, hashboardId: 12n, moduleId: 1n, deviceId: 10n },
      ],
    }),
  );
  const view = () => (
    <PoolAssignments moduleIds={[1n]}>
      <BoardPoolConfigurationCell moduleId={1n} />
      <BoardPoolConfigurationCell hashboardId={11n} />
    </PoolAssignments>
  );
  const first = render(view());
  expect(await screen.findByText("Needs attention")).toBeVisible();
  expect(screen.queryByText("Production")).not.toBeInTheDocument();
  expect(mocks.getAssignments).toHaveBeenCalledTimes(1);
  first.unmount();
  render(view());
  expect(await screen.findByText("Needs attention")).toBeVisible();
});

it("distinguishes device pools, confirmed empty settings, and unreadable settings", async () => {
  mocks.getAssignments.mockResolvedValue(
    create(GetAssignmentsResponseSchema, {
      selectedCount: 3n,
      assignments: [
        { deviceId: 1n, deviceIdentifier: "miner-a", state: "unavailable" },
        { deviceId: 2n, deviceIdentifier: "miner-b", state: "unavailable" },
        { deviceId: 3n, deviceIdentifier: "miner-c", state: "unavailable" },
      ],
    }),
  );
  mocks.readDevicePools.mockImplementation(({ targets }) =>
    Promise.resolve({
      readings: targets.map((target: { kind: string; targetId: bigint }) => ({
        target,
        reading: create(CheckConfigurationResponseSchema, {
          state: target.targetId === 3n ? "unavailable" : target.targetId === 1n ? "device_configuration" : "",
          readbackAvailable: target.targetId !== 3n,
        }),
      })),
    }),
  );
  render(
    <PoolAssignments deviceIdentifiers={["miner-a", "miner-b", "miner-c"]}>
      <PoolConfigurationCell deviceIdentifier="miner-a" />
      <PoolConfigurationCell deviceIdentifier="miner-b" />
      <PoolConfigurationCell deviceIdentifier="miner-c" />
    </PoolAssignments>,
  );
  expect(await screen.findByText("Device configuration")).toBeVisible();
  expect(await screen.findByText("—")).toBeVisible();
  expect(screen.getByRole("button", { name: "Unavailable" })).toBeVisible();
  expect(mocks.readDevicePools).toHaveBeenCalledTimes(1);
});

const RefreshAssignments = () => <button onClick={useSavedPoolAssignments()?.refresh}>Refresh pools</button>;

it.each(["focus", "manual", "poll"])("refreshes device-only readings for unchanged targets on %s", async (trigger) => {
  mocks.getAssignments.mockResolvedValue(
    create(GetAssignmentsResponseSchema, {
      assignments: [{ deviceId: 1n, deviceIdentifier: "miner-a", state: "unavailable" }],
    }),
  );
  mocks.readDevicePools.mockRejectedValueOnce(new Error("offline")).mockResolvedValue({
    readings: [
      {
        target: { kind: "device", targetId: 1n },
        reading: create(CheckConfigurationResponseSchema, { state: "device_configuration", readbackAvailable: true }),
      },
    ],
  });
  vi.useFakeTimers();
  const view = render(
    <PoolAssignments deviceIdentifiers={["miner-a"]}>
      <PoolConfigurationCell deviceIdentifier="miner-a" />
      <RefreshAssignments />
    </PoolAssignments>,
  );
  try {
    await act(async () => {});
    expect(mocks.readDevicePools).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Unavailable" })).toBeVisible();
    await act(async () => {
      if (trigger === "focus") fireEvent.focus(window);
      else if (trigger === "manual") fireEvent.click(screen.getByRole("button", { name: "Refresh pools" }));
      else await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(screen.getByRole("button", { name: "Device configuration" })).toBeVisible();
    expect(mocks.readDevicePools).toHaveBeenCalledTimes(2);
  } finally {
    view.unmount();
    vi.useRealTimers();
  }
});

it("shows verified and pool-free boards as mixed and refreshes after removal", async () => {
  const assignments = [
    {
      deviceId: 10n,
      installationId: 101n,
      hashboardId: 11n,
      moduleId: 1n,
      configurationId: "config",
      name: "Default",
      state: "verified",
    },
    { deviceId: 10n, installationId: 102n, hashboardId: 12n, moduleId: 1n },
  ];
  mocks.getAssignments.mockResolvedValue(create(GetAssignmentsResponseSchema, { assignments }));
  mocks.readDevicePools.mockImplementation(async ({ targets }) => ({
    readings: targets.map((target: { kind: string; targetId: bigint }) => ({
      target,
      reading: create(CheckConfigurationResponseSchema, { state: "", readbackAvailable: true }),
    })),
  }));
  render(
    <PoolAssignments moduleIds={[1n]}>
      <BoardPoolConfigurationCell moduleId={1n} />
    </PoolAssignments>,
  );
  expect(await screen.findByRole("button", { name: "Mixed configurations" })).toBeVisible();
  expect(screen.queryByText("Needs attention")).not.toBeInTheDocument();
  mocks.getAssignments.mockResolvedValue(
    create(GetAssignmentsResponseSchema, {
      assignments: assignments.map((a) => ({ ...a, configurationId: "", name: "", state: "" })),
    }),
  );
  act(() => refreshPoolAssignments());
  expect(await screen.findByText("—")).toBeVisible();
  expect(screen.queryByText("Mixed configurations")).not.toBeInTheDocument();
});
