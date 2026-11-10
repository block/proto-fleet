import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampDate, TimestampSchema } from "@bufbuild/protobuf/wkt";
import { type Client, Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";
import { PoolAssignmentsContext } from "./assignmentContext";
import { trackPoolOperation, usePoolAssignmentProgress } from "./assignmentProgress";
import { BoardPoolConfigurationCell } from "./BoardPoolConfigurationCell";
import { OperationResults } from "./OperationResults";
import { PoolAssignmentFeedback } from "./PoolAssignmentFeedback";
import { PoolConfigurationCell } from "./PoolConfigurationCell";
import {
  CheckConfigurationResponseSchema,
  ConfigurationSchema,
  EquipmentAssignmentSchema,
  GetAssignmentsResponseSchema,
  GetOperationResponseSchema,
  type PoolConfigurationService,
  ReviewedEquipmentSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

const mocks = vi.hoisted(() => ({
  getAssignments: vi.fn(),
  getOperation: vi.fn(),
  checkConfiguration: vi.fn(),
  readDevicePools: vi.fn(),
  reviewRetry: vi.fn(),
  accountId: "",
  read: true,
}));
vi.mock("./api", () => ({
  usePoolConfigurationClient: () => ({ client: mocks, accountId: mocks.accountId, readOnly: false }),
  usePoolConfigurationPermissions: () => ({ read: mocks.read, change: true }),
}));
beforeEach(() => {
  vi.clearAllMocks();
  mocks.accountId = "";
  mocks.read = true;
  usePoolAssignmentProgress.setState({ jobs: [], detailsJobId: undefined, detailsSelection: undefined });
});
const operation = (state: string, resultCode: string) =>
  create(GetOperationResponseSchema, {
    configurationId: "config",
    configurationName: "default 2",
    operationId: "operation",
    targets: [{ kind: "device", targetId: 38n, state, resultCode, label: "02:42:11:0F:91:B3" }],
  });
const show = () => render(<OperationResults operationId="operation" onOperationChange={vi.fn()} onDismiss={vi.fn()} />);
it.each([
  ["miner", true],
  ["module", true],
  ["hashboard", true],
  ["miner", false],
  ["module", false],
  ["hashboard", false],
] as const)(
  "explains an out-of-scope %s with read permission %s without claiming it has no pools",
  async (kind, readable) => {
    mocks.read = readable;
    mocks.getAssignments.mockResolvedValue(create(GetAssignmentsResponseSchema));
    render(
      <>
        <PoolAssignmentsContext.Provider
          value={{
            byIdentifier: new Map(),
            byId: new Map(),
            boards: [],
            readable,
            loading: false,
            error: null,
            refresh: vi.fn(),
          }}
        >
          {kind === "miner" ? (
            <PoolConfigurationCell deviceId={38n} />
          ) : kind === "module" ? (
            <BoardPoolConfigurationCell moduleId={10n} />
          ) : (
            <BoardPoolConfigurationCell hashboardId={11n} />
          )}
        </PoolAssignmentsContext.Provider>
        <PoolAssignmentFeedback />
      </>,
    );
    await userEvent.setup().click(screen.getByRole("button", { name: "Unavailable" }));
    await waitFor(() =>
      expect(screen.getByText(/You don’t have access to view this hardware’s pool configuration/)).toBeVisible(),
    );
    expect(screen.queryByText(/No saved pool configuration is assigned/)).not.toBeInTheDocument();
    expect(mocks.readDevicePools).not.toHaveBeenCalled();
    if (!readable) expect(mocks.getAssignments).not.toHaveBeenCalled();
  },
);

it("keeps assignment lookup failures separate from allocation access", async () => {
  mocks.getAssignments.mockRejectedValue(new ConnectError("offline", Code.Unavailable));
  usePoolAssignmentProgress.setState({ detailsSelection: { accountId: "", deviceIds: [38n] } });
  render(<PoolAssignmentFeedback />);
  expect(
    await screen.findByText("Fleet could not load the update status. The update may still be running."),
  ).toBeVisible();
  expect(
    screen.queryByText(/You don’t have access to view this hardware’s pool configuration/),
  ).not.toBeInTheDocument();
});

it("explains denied assignment access without implying an update failed", async () => {
  mocks.getAssignments.mockRejectedValue(new ConnectError("denied", Code.PermissionDenied));
  usePoolAssignmentProgress.setState({ detailsSelection: { accountId: "", deviceIds: [38n] } });
  render(<PoolAssignmentFeedback />);
  expect(await screen.findByText(/You don’t have access to view this hardware’s pool configuration/)).toBeVisible();
  expect(screen.queryByText(/The update may still be running/)).not.toBeInTheDocument();
});

it("explains uncertainty without offering a blind retry", async () => {
  mocks.getOperation.mockResolvedValue(operation("failed", "write_uncertain"));
  show();
  expect((await screen.findByText("default 2", { exact: true })).parentElement).toHaveTextContent(
    "Configuration: default 2",
  );
  expect(screen.getByText("Not verified")).toBeVisible();
  expect(screen.getByText("Fleet couldn’t confirm that an earlier device update finished.")).toBeVisible();
  expect(
    screen.getByText(
      "Fleet couldn’t confirm that an earlier device update finished. Another pool update is blocked until that is resolved.",
    ),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "Reapply" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Check configuration" })).toBeVisible();
});
it("shows the concrete failure reason with Retry", async () => {
  mocks.getOperation.mockResolvedValue(operation("failed", "unavailable"));
  show();
  await waitFor(() =>
    expect(screen.getByText("Fleet could not access the equipment to apply this configuration.")).toBeVisible(),
  );
  expect(screen.getByRole("button", { name: "Retry" })).toBeVisible();
});
it("reads actual settings to show differences and never submits a mutation", async () => {
  mocks.getOperation.mockResolvedValue(operation("different", "settings_differ"));
  mocks.checkConfiguration.mockResolvedValue(
    create(CheckConfigurationResponseSchema, {
      state: "different",
      resultCode: "settings_differ",
      expected: [{ url: "stratum+tcp://expected", username: "expected.worker" }],
      reported: [{ url: "stratum+tcp://actual", username: "actual.worker" }],
    }),
  );
  const user = userEvent.setup();
  show();
  await user.click(await screen.findByRole("button", { name: "Check configuration" }));
  expect(await screen.findByRole("table", { name: "Primary pool settings" })).toBeVisible();
  expect(screen.getByRole("cell", { name: /stratum\+tcp:\/\/actual/ })).toBeVisible();
  expect(mocks.checkConfiguration).toHaveBeenCalledWith(
    expect.objectContaining({ configurationId: "config", target: { kind: "device", targetId: 38n } }),
    expect.anything(),
  );
  expect(mocks.reviewRetry).not.toHaveBeenCalled();
});
it("keeps a failed status read separate from a failed device update", async () => {
  mocks.getOperation.mockRejectedValue(new Error("network unavailable"));
  show();
  await waitFor(() =>
    expect(screen.getByText("Fleet could not load the update status. The update may still be running.")).toBeVisible(),
  );
  expect(screen.getByRole("button", { name: "Refresh status" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
});
it("refreshes an accepted operation with unavailable progress without offering a local resubmit", async () => {
  mocks.getOperation.mockRejectedValue(new ConnectError("temporarily missing", Code.NotFound));
  await trackPoolOperation(
    mocks as unknown as Client<typeof PoolConfigurationService>,
    "",
    create(ConfigurationSchema, { name: "default 2", configurationId: "config" }),
    "operation",
    [create(ReviewedEquipmentSchema, { kind: "device", targetId: 38n })],
  );
  const job = usePoolAssignmentProgress.getState().jobs[0];
  expect(job.phase).toBe("unknown");
  usePoolAssignmentProgress.setState({ detailsJobId: job.id });
  render(<PoolAssignmentFeedback />);
  await waitFor(() =>
    expect(screen.getByText("Fleet could not load the update status. The update may still be running.")).toBeVisible(),
  );
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  mocks.getOperation.mockResolvedValue(operation("verified", ""));
  await userEvent.setup().click(screen.getByRole("button", { name: "Refresh status" }));
  expect(await screen.findByText("Verified")).toBeVisible();
  expect(mocks.getOperation).toHaveBeenLastCalledWith({ accountId: "", operationId: "operation" }, expect.anything());
  expect(mocks.reviewRetry).not.toHaveBeenCalled();
});

it("keeps problem details open when a table refresh removes its row", async () => {
  const a = create(EquipmentAssignmentSchema, {
    deviceId: 38n,
    deviceIdentifier: "miner-a",
    configurationId: "config",
    name: "default 2",
    operationId: "operation",
    state: "failed",
    resultCode: "unavailable",
  });
  mocks.getAssignments.mockResolvedValue(create(GetAssignmentsResponseSchema, { assignments: [a] }));
  mocks.getOperation.mockResolvedValue(operation("failed", "unavailable"));
  const host = <PoolAssignmentFeedback />;
  const view = render(
    <>
      <PoolAssignmentsContext.Provider
        value={{
          byIdentifier: new Map([["miner-a", a]]),
          byId: new Map([["38", a]]),
          readable: true,
          loadedAt: 3000,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <PoolConfigurationCell deviceIdentifier="miner-a" />
      </PoolAssignmentsContext.Provider>
      {host}
    </>,
  );
  await userEvent.setup().click(screen.getByRole("button", { name: "Update failed" }));
  await waitFor(() =>
    expect(screen.getByText("Fleet could not access the equipment to apply this configuration.")).toBeVisible(),
  );
  view.rerender(
    <>
      {null}
      {host}
    </>,
  );
  await waitFor(() => expect(screen.getByRole("dialog", { name: "Pool configuration" })).toBeVisible());
  await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).toBeVisible());
});

it("shows a known mismatch separately from a blocked reapply", async () => {
  mocks.getOperation.mockResolvedValue(operation("different", "write_uncertain"));
  mocks.checkConfiguration.mockResolvedValue(
    create(CheckConfigurationResponseSchema, {
      state: "different",
      resultCode: "write_uncertain",
      readbackAvailable: true,
      expected: [{ url: "stratum+tcp://expected", username: "wanted" }],
      reported: [{ url: "stratum+tcp://actual", username: "actual" }],
    }),
  );
  show();
  await userEvent.setup().click(await screen.findByRole("button", { name: "Check configuration" }));
  expect(await screen.findByRole("table", { name: "Primary pool settings" })).toBeVisible();
  expect(screen.getByRole("heading", { name: "Settings differ" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Reapply" })).toBeDisabled();
  expect(screen.queryByText("Not verified")).not.toBeInTheDocument();
});

it.each([
  ["miner", "device_configuration", "Device configuration"],
  ["module", "device_configuration", "Device configuration"],
  ["hashboard", "device_configuration", "Device configuration"],
  ["module", "container_configuration", "Container configuration"],
  ["hashboard", "container_configuration", "Container configuration"],
])("opens reported settings from a %s row with %s", async (kind, state, label) => {
  const board = kind !== "miner";
  const assignment = create(EquipmentAssignmentSchema, {
    deviceId: 38n,
    deviceIdentifier: "miner-a",
    deviceName: "Device Alpha",
    state,
    ...(board ? { installationId: 101n, moduleId: 10n, hashboardId: 11n } : {}),
  });
  mocks.getAssignments.mockResolvedValue(create(GetAssignmentsResponseSchema, { assignments: [assignment] }));
  mocks.readDevicePools.mockResolvedValue({
    readings: [
      {
        target: { kind: board ? "hashboard_installation" : "device", targetId: board ? 101n : 38n },
        reading: create(CheckConfigurationResponseSchema, {
          state,
          readbackAvailable: true,
          reported: [
            { url: "stratum+tcp://primary.example:3333", username: "primary.worker" },
            { url: "stratum+tcp://backup.example:3333", username: "backup.worker", priority: 1 },
          ],
        }),
      },
    ],
  });
  render(
    <>
      <PoolAssignmentsContext.Provider
        value={{
          byIdentifier: new Map([["miner-a", assignment]]),
          byId: new Map([["38", assignment]]),
          boards: board ? [assignment] : [],
          readable: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        {kind === "miner" ? (
          <PoolConfigurationCell deviceIdentifier="miner-a" />
        ) : kind === "module" ? (
          <BoardPoolConfigurationCell moduleId={10n} />
        ) : (
          <BoardPoolConfigurationCell hashboardId={11n} />
        )}
      </PoolAssignmentsContext.Provider>
      <PoolAssignmentFeedback />
    </>,
    { wrapper: MemoryRouter },
  );
  const link = screen.getByRole("button", { name: label });
  expect(link).toHaveClass("hover:underline");
  expect(link).not.toHaveClass("underline");
  const user = userEvent.setup();
  await user.click(link);
  const dialog = within(await screen.findByRole("dialog", { name: "Pool configuration" }));
  await waitFor(() => expect(dialog.getByText("primary.worker")).toBeVisible());
  expect(dialog.getByText("stratum+tcp://primary.example:3333")).toBeVisible();
  if (state === "container_configuration")
    expect(
      dialog.getByText(
        "Mining groups are not enabled on this container. These hashboards use the whole container’s pool configuration.",
      ),
    ).toBeVisible();
  expect(dialog.queryByText("Details")).not.toBeInTheDocument();
  expect(dialog.getByRole("heading", { name: "Backup pool 1" })).toBeVisible();
  expect(dialog.getByText("stratum+tcp://backup.example:3333")).toBeVisible();
  expect(dialog.getByText("backup.worker")).toBeVisible();
  expect(mocks.readDevicePools).toHaveBeenCalledWith(
    { accountId: "", targets: [{ kind: board ? "hashboard_installation" : "device", targetId: board ? 101n : 38n }] },
    expect.anything(),
  );
  expect(mocks.getOperation).not.toHaveBeenCalled();
  expect(mocks.reviewRetry).not.toHaveBeenCalled();
});

it("shows each board only under its current assignment's operation", async () => {
  const earlier = create(GetOperationResponseSchema, {
    operationId: "module-operation",
    configurationId: "config",
    configurationName: "Original",
    targets: [101n, 102n, 103n, 999n].map((targetId) => ({
      kind: "hashboard_installation",
      targetId,
      label: `Board ${targetId}`,
      state: "verified",
    })),
  });
  const latest = create(GetOperationResponseSchema, {
    operationId: "board-operation",
    configurationId: "replacement",
    configurationName: "Replacement",
    targets: [
      {
        kind: "hashboard_installation",
        targetId: 101n,
        label: "Board 101",
        state: "failed",
        resultCode: "unavailable",
      },
    ],
  });
  mocks.getOperation.mockImplementation(({ operationId }) =>
    Promise.resolve(operationId === earlier.operationId ? earlier : latest),
  );
  const assignments = [101n, 102n, 103n].map((installationId) =>
    create(EquipmentAssignmentSchema, {
      installationId,
      deviceId: 38n,
      moduleId: 10n,
      deviceName: `Board ${installationId}`,
      operationId: installationId === 101n ? latest.operationId : earlier.operationId,
      configurationId: installationId === 101n ? latest.configurationId : earlier.configurationId,
      state: installationId === 101n ? "failed" : "verified",
      resultCode: installationId === 101n ? "unavailable" : "",
    }),
  );
  render(<OperationResults assignments={assignments} onOperationChange={vi.fn()} onDismiss={vi.fn()} />);
  await screen.findByRole("heading", { name: "Update failed" });
  expect(screen.getAllByRole("region", { name: "Board 101" })).toHaveLength(1);
  expect(
    within(screen.getByRole("region", { name: "Board 101" })).getByRole("heading", { name: "Update failed" }),
  ).toBeVisible();
  expect(screen.getAllByRole("heading", { name: "Verified" })).toHaveLength(2);
  expect(screen.queryByRole("region", { name: "Board 999" })).not.toBeInTheDocument();
});

it("preserves all operation targets when viewing explicit history", async () => {
  mocks.getOperation.mockResolvedValue(
    create(GetOperationResponseSchema, {
      operationId: "original",
      configurationId: "config",
      configurationName: "Original",
      targets: [101n, 102n].map((targetId) => ({
        kind: "hashboard_installation",
        targetId,
        label: `Board ${targetId}`,
        state: "verified",
      })),
    }),
  );
  render(
    <OperationResults
      operationId="original"
      assignments={[
        create(EquipmentAssignmentSchema, {
          installationId: 101n,
          configurationId: "new-config",
          operationId: "new-operation",
          state: "failed",
        }),
      ]}
      onOperationChange={vi.fn()}
      onDismiss={vi.fn()}
    />,
  );
  expect(await screen.findAllByRole("heading", { name: "Verified" })).toHaveLength(2);
  expect(screen.getByRole("region", { name: "Board 101" })).toBeVisible();
  expect(screen.getByRole("region", { name: "Board 102" })).toBeVisible();
});

it("invalidates a cached mismatch when the operation is superseded by another update", async () => {
  const initial = operation("different", "settings_differ");
  mocks.getOperation.mockResolvedValue(initial);
  mocks.checkConfiguration.mockResolvedValue(
    create(CheckConfigurationResponseSchema, {
      state: "different",
      resultCode: "settings_differ",
      checkedAt: { seconds: 3n, nanos: 0 },
      reported: [{ url: "stratum+tcp://old.example:3333", username: "old.worker" }],
    }),
  );
  show();
  await userEvent.setup().click(await screen.findByRole("button", { name: "Check configuration" }));
  expect(await screen.findByRole("button", { name: "Reapply" })).toBeVisible();
  mocks.getOperation.mockResolvedValue(operation("superseded", "write_uncertain"));
  await waitFor(() => expect(screen.getByRole("heading", { name: "Replaced by a newer update" })).toBeVisible(), {
    timeout: 4500,
  });
  expect(screen.queryByRole("button", { name: "Reapply" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Check again" })).not.toBeInTheDocument();
  expect(screen.queryByText("old.worker")).not.toBeInTheDocument();
});

it("keeps a fresh check over older polls but discards it for newer assignment evidence", async () => {
  mocks.getOperation.mockResolvedValue(operation("failed", "unavailable"));
  const assignment = create(EquipmentAssignmentSchema, {
    deviceId: 38n,
    configurationId: "config",
    operationId: "operation",
    state: "failed",
    resultCode: "unavailable",
    updatedAt: { seconds: 1n, nanos: 0 },
  });
  mocks.checkConfiguration.mockResolvedValue(
    create(CheckConfigurationResponseSchema, {
      state: "verified",
      checkedAt: { seconds: 3n, nanos: 0 },
      reported: [{ url: "stratum+tcp://pool.example:3333", username: "verified.worker" }],
    }),
  );
  const props = { onOperationChange: vi.fn(), onDismiss: vi.fn() };
  const view = render(<OperationResults {...props} assignments={[assignment]} />);
  await userEvent.setup().click(await screen.findByRole("button", { name: "Check configuration" }));
  expect(await screen.findByRole("heading", { name: "Verified" })).toBeVisible();
  view.rerender(
    <OperationResults
      {...props}
      assignments={[
        create(EquipmentAssignmentSchema, {
          ...assignment,
          updatedAt: create(TimestampSchema, { seconds: 2n, nanos: 0 }),
        }),
      ]}
    />,
  );
  expect(screen.getByRole("heading", { name: "Verified" })).toBeVisible();
  view.rerender(
    <OperationResults
      {...props}
      assignments={[
        create(EquipmentAssignmentSchema, {
          ...assignment,
          state: "verified",
          resultCode: "",
          updatedAt: create(TimestampSchema, { seconds: 3n, nanos: 0 }),
        }),
      ]}
    />,
  );
  expect(screen.getByText("verified.worker")).toBeVisible();
  view.rerender(
    <OperationResults
      {...props}
      assignments={[
        create(EquipmentAssignmentSchema, {
          ...assignment,
          updatedAt: create(TimestampSchema, { seconds: 3n, nanos: 1 }),
        }),
      ]}
    />,
  );
  expect(screen.getByRole("heading", { name: "Update failed" })).toBeVisible();
  expect(screen.queryByText("verified.worker")).not.toBeInTheDocument();
});

it("opens verified configs with the current assignment name and latest verification time", async () => {
  const originalVerifiedAt = create(TimestampSchema, { seconds: 1700000000n });
  const lastVerifiedAt = create(TimestampSchema, { seconds: 1700003600n });
  const assignment = create(EquipmentAssignmentSchema, {
    deviceId: 38n,
    deviceIdentifier: "miner-a",
    deviceName: "Device Alpha",
    configurationId: "config",
    name: "Current production",
    operationId: "operation",
    state: "verified",
    updatedAt: lastVerifiedAt,
  });
  const original = operation("verified", "");
  original.targets[0].updatedAt = originalVerifiedAt;
  mocks.getAssignments.mockResolvedValue(create(GetAssignmentsResponseSchema, { assignments: [assignment] }));
  mocks.getOperation.mockResolvedValue(original);
  render(
    <>
      <PoolAssignmentsContext.Provider
        value={{
          byIdentifier: new Map([["miner-a", assignment]]),
          byId: new Map([["38", assignment]]),
          readable: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <PoolConfigurationCell deviceIdentifier="miner-a" />
      </PoolAssignmentsContext.Provider>
      <PoolAssignmentFeedback />
    </>,
    { wrapper: MemoryRouter },
  );
  await userEvent.click(screen.getByRole("button", { name: "Current production" }));
  const dialog = within(await screen.findByRole("dialog", { name: "Pool configuration" }));
  await waitFor(() => expect(dialog.getByRole("heading", { name: "Verified" })).toBeVisible());
  expect(dialog.getByText("Current production").parentElement).toHaveTextContent("Configuration: Current production");
  expect(dialog.getByText("Last verified").nextElementSibling).toHaveTextContent(
    timestampDate(lastVerifiedAt).toLocaleString(),
  );
  expect(dialog.queryByText("default 2", { exact: true })).not.toBeInTheDocument();
  expect(mocks.getAssignments).toHaveBeenCalledWith(
    expect.objectContaining({ deviceIdentifiers: ["miner-a"] }),
    expect.anything(),
  );
  expect(mocks.checkConfiguration).not.toHaveBeenCalled();
  expect(dialog.queryByRole("button", { name: "View miner" })).not.toBeInTheDocument();
  expect(dialog.queryByRole("link", { name: "View container" })).not.toBeInTheDocument();
  expect(dialog.getByRole("button", { name: "Check configuration" })).toBeVisible();
});

it("updates Last verified after a successful configuration check", async () => {
  const result = operation("verified", "");
  result.targets[0].updatedAt = create(TimestampSchema, { seconds: 1700000000n });
  const checkedAt = create(TimestampSchema, { seconds: 1700007200n });
  mocks.getOperation.mockResolvedValue(result);
  mocks.checkConfiguration.mockResolvedValue(
    create(CheckConfigurationResponseSchema, { state: "verified", checkedAt }),
  );
  show();
  await userEvent.click(await screen.findByRole("button", { name: "Check configuration" }));
  await waitFor(() =>
    expect(screen.getByText("Last verified").nextElementSibling).toHaveTextContent(
      timestampDate(checkedAt).toLocaleString(),
    ),
  );
});

it("keeps Last verified at the earlier success when a newer check reports different settings", async () => {
  const verifiedAt = create(TimestampSchema, { seconds: 1700000000n });
  const checkedAt = create(TimestampSchema, { seconds: 1700007200n });
  const result = operation("verified", "");
  result.targets[0].updatedAt = verifiedAt;
  mocks.getOperation.mockResolvedValue(result);
  mocks.checkConfiguration.mockResolvedValue(
    create(CheckConfigurationResponseSchema, { state: "different", resultCode: "settings_differ", checkedAt }),
  );
  show();
  expect(await screen.findByRole("heading", { name: "Verified" })).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "Check configuration" }));
  expect(await screen.findByRole("heading", { name: "Settings differ" })).toBeVisible();
  expect(screen.getByText("Last verified").nextElementSibling).toHaveTextContent(
    timestampDate(verifiedAt).toLocaleString(),
  );
});

it.each([
  ["clear", "completed", "—", "Device pools cleared and Fleet assignment removed."],
  ["unassign", "completed", "Assignment removed", "Fleet assignment removed. Device pool settings were preserved."],
  [
    "clear",
    "failed",
    "Removal failed",
    "Removal failed. The Fleet assignment was kept. Review the device’s settings before trying again.",
  ],
  [
    "clear",
    "uncertain",
    "Removal not confirmed",
    "Fleet has not confirmed removal. Check its result before another pool change.",
  ],
])("shows %s/%s history without reapplying the old configuration", async (kind, state, status, explanation) => {
  mocks.getOperation.mockResolvedValue(create(GetOperationResponseSchema, { ...operation(state, ""), kind }));
  show();
  expect(await screen.findByText(status)).toBeInTheDocument();
  expect(screen.getByText(explanation)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reapply" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Check configuration" })).not.toBeInTheDocument();
});

it.each([
  ["operator_recovery_required", "Administrator recovery required"],
  ["runtime_pools_remain", "Runtime pools still present"],
  ["clear_verification_unavailable", "Waiting for verification"],
])("explains removal recovery result %s in operation history", async (code, label) => {
  mocks.getOperation.mockResolvedValue(
    create(GetOperationResponseSchema, { ...operation("uncertain", code), kind: "clear" }),
  );
  show();
  expect(await screen.findByRole("heading", { name: label })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reapply" })).not.toBeInTheDocument();
});
