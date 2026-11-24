import { act, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { ConfigurationEditor } from "./ConfigurationEditor";
import { ConfigurationSelector } from "./ConfigurationSelector";
import { useConfirmationRecovery } from "./confirmationAttempts";
import { configurationChanges, configurationDraft, poolInput } from "./draft";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  ConfigurationSchema,
  EquipmentAssignmentSchema,
  GetOperationResponseSchema,
  ReviewAssignmentResponseSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({
  client: {
    createConfiguration: vi.fn(),
    listConfigurations: vi.fn(),
    getAssignments: vi.fn(),
    reviewEdit: vi.fn(),
    reviewAssignment: vi.fn(),
    confirmReview: vi.fn(),
    getOperation: vi.fn(),
    testConnection: vi.fn(),
    authorizeReviews: vi.fn(),
  },
  accountId: "customer",
  permissions: { manage: true, change: true },
  track: vi.fn(),
  verifyCredentials: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({ authClient: { verifyCredentials: mocks.verifyCredentials } }));
vi.mock("./assignmentProgress", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./assignmentProgress")>()),
  trackPoolOperation: mocks.track,
}));
vi.mock("./api", () => ({
  usePoolConfigurationClient: () => ({ client: mocks.client, accountId: mocks.accountId, readOnly: false }),
  usePoolConfigurationPermissions: () => mocks.permissions,
}));
const configuration = create(ConfigurationSchema, {
  configurationId: "config-1",
  name: "Production",
  version: 3n,
  assignedCount: 2n,
  pools: [
    {
      slotId: "slot-1",
      url: "stratum+tcp://pool.example",
      username: "payout",
      hasPassword: true,
      workerTemplate: "{miner_name}",
    },
  ],
});

beforeEach(() => {
  vi.clearAllMocks();
  useConfirmationRecovery.setState({ attempts: [] });
  mocks.accountId = "customer";
  mocks.verifyCredentials.mockReset().mockResolvedValue({});
  mocks.permissions.manage = true;
  mocks.permissions.change = true;
  mocks.client.listConfigurations.mockResolvedValue({ configurations: [configuration], nextPageToken: "" });
  mocks.client.confirmReview.mockResolvedValue({ configuration, operationId: "operation-1" });
  mocks.client.getOperation.mockResolvedValue({ targets: [] });
  mocks.client.getAssignments.mockResolvedValue({ assignments: [], selectedCount: 1n });
  mocks.client.reviewAssignment.mockReset().mockResolvedValue({
    reviewId: "apply-1",
    configuration,
    affectedMiners: 2n,
    affectedHashboards: 0n,
    equipment: [],
  });
});

const authenticateForPools = async (user: ReturnType<typeof userEvent.setup>) => {
  expect(screen.queryByRole("dialog", { name: "Edit pool" })).not.toBeInTheDocument();
  await user.type(screen.getByLabelText("Username"), "pool-operator");
  await user.type(screen.getByLabelText("Password"), "actor-password");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("dialog", { name: "Edit pool" });
  expect(screen.queryByLabelText("Your password")).not.toBeInTheDocument();
};

it("keeps removal results visible while refreshing the equipment assignments", async () => {
  const user = userEvent.setup();
  const onDismiss = vi.fn();
  mocks.client.getAssignments.mockResolvedValueOnce({
    selectedCount: 1n,
    assignments: [create(EquipmentAssignmentSchema, { deviceId: 7n, configurationId: configuration.configurationId })],
  });
  mocks.client.reviewAssignment.mockResolvedValue(
    create(ReviewAssignmentResponseSchema, {
      reviewId: "removal-1",
      removal: { verificationState: "verified", clearSupported: true },
    }),
  );
  mocks.client.authorizeReviews.mockResolvedValue({});
  mocks.client.confirmReview.mockResolvedValue({ operationId: "removal-1" });
  mocks.client.getOperation.mockResolvedValue(
    create(GetOperationResponseSchema, {
      operationId: "removal-1",
      kind: "clear",
      targets: [{ kind: "device", targetId: 7n, state: "completed" }],
    }),
  );
  render(<ConfigurationSelector deviceIds={[7n]} onDismiss={onDismiss} />);
  await authenticateForPools(user);
  await user.click(await screen.findByRole("button", { name: "Remove pool configuration" }));
  const confirm = screen.getByRole("button", { name: "Clear pools and remove assignment" });
  await waitFor(() => expect(confirm).toBeEnabled());
  await user.click(confirm);
  await waitFor(() => expect(mocks.client.getAssignments).toHaveBeenCalledTimes(2));
  expect(await screen.findByText("Device pools cleared and Fleet assignment removed.")).toBeVisible();
  expect(mocks.client.reviewAssignment).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(onDismiss).toHaveBeenCalledOnce();
  expect(screen.queryByRole("dialog", { name: "Edit pool" })).not.toBeInTheDocument();
});

describe("shared pool configuration flows", () => {
  it("trims pasted pool URLs before connection testing and saving", async () => {
    const user = userEvent.setup();
    mocks.client.testConnection.mockResolvedValue({ connected: true });
    mocks.client.createConfiguration.mockResolvedValue({ configuration });
    render(<ConfigurationEditor onSaved={vi.fn()} onDismiss={vi.fn()} />);
    await user.type(screen.getByLabelText("Name"), "Pasted pools");
    await user.click(screen.getByRole("button", { name: "Add backup pool" }));
    const urls = ["stratum+tcp://pool.example:3333", "stratum+tcp://backup.example:3333"];
    for (const [index, label] of ["Primary pool", "Backup 1"].entries()) {
      const slot = within(screen.getByRole("group", { name: label }));
      await user.click(slot.getByLabelText("Pool URL"));
      await user.paste(` ${urls[index]} `);
      await user.type(slot.getByLabelText("Username"), "payout");
      await user.click(slot.getByRole("button", { name: "Test connection" }));
      await slot.findByText("Pool connection successful");
    }
    expect(mocks.client.testConnection.mock.calls.map(([request]) => request.pool.url)).toEqual(urls);
    await user.click(screen.getByRole("button", { name: "Save configuration" }));
    await waitFor(() => expect(mocks.client.createConfiguration).toHaveBeenCalledOnce());
    expect(
      mocks.client.createConfiguration.mock.calls[0][0].configuration.pools.map((pool: { url: string }) => pool.url),
    ).toEqual(urls);
    expect(screen.getAllByLabelText("Pool URL").map((input) => (input as HTMLInputElement).value)).toEqual(urls);
  });

  it.each(["Primary pool", "Backup 1"])(
    "disables saving and connection testing for a whitespace-only %s URL",
    async (label) => {
      const user = userEvent.setup();
      render(<ConfigurationEditor onSaved={vi.fn()} onDismiss={vi.fn()} />);
      await user.type(screen.getByLabelText("Name"), "Pasted pools");
      await user.click(screen.getByRole("button", { name: "Add backup pool" }));
      for (const name of ["Primary pool", "Backup 1"]) {
        const slot = within(screen.getByRole("group", { name }));
        await user.type(slot.getByLabelText("Pool URL"), "stratum+tcp://pool.example:3333");
        await user.type(slot.getByLabelText("Username"), "payout");
      }
      expect(screen.getByRole("button", { name: "Save configuration" })).toBeEnabled();
      const slot = within(screen.getByRole("group", { name: label }));
      await user.clear(slot.getByLabelText("Pool URL"));
      await user.paste("   ");
      expect(screen.getByRole("button", { name: "Save configuration" })).toBeDisabled();
      expect(slot.getByRole("button", { name: "Test connection" })).toBeDisabled();
      expect(slot.getByLabelText("Pool URL")).toHaveValue("");
      expect(mocks.client.createConfiguration).not.toHaveBeenCalled();
      expect(mocks.client.testConnection).not.toHaveBeenCalled();
    },
  );

  it("preselects the saved configuration after reopening the picker", async () => {
    mocks.client.getAssignments.mockResolvedValue({
      selectedCount: 2n,
      assignments: [
        { configurationId: configuration.configurationId },
        { configurationId: configuration.configurationId },
      ],
    });
    const user = userEvent.setup();
    render(<ConfigurationSelector deviceIdentifiers={["miner-a", "miner-b"]} onDismiss={vi.fn()} />);
    await authenticateForPools(user);
    await waitFor(() => expect(screen.getByRole("radio", { name: /Production/ })).toBeChecked());
    expect(screen.getByRole("button", { name: "Assign" })).toBeEnabled();
  });

  it.each([[{ configurationId: "config-1" }, { configurationId: "other" }], [{ configurationId: "config-1" }]])(
    "leaves mixed or partly unmanaged selections unselected",
    async (...assignments) => {
      mocks.client.getAssignments.mockResolvedValue({ selectedCount: 2n, assignments });
      const user = userEvent.setup();
      render(<ConfigurationSelector deviceIdentifiers={["miner-a", "miner-b"]} onDismiss={vi.fn()} />);
      await authenticateForPools(user);
      await waitFor(() => expect(screen.getByText("Mixed configurations")).toBeVisible());
      expect(screen.getByRole("radio", { name: /Production/ })).not.toBeChecked();
      expect(screen.getByRole("button", { name: "Assign" })).toBeDisabled();
    },
  );

  it("tests a saved slot without revealing its password and clears stale results when fields change", async () => {
    const user = userEvent.setup();
    mocks.client.testConnection.mockResolvedValue({ connected: true });
    render(<ConfigurationEditor original={configuration} onSaved={vi.fn()} onDismiss={vi.fn()} />);
    const primary = within(screen.getByRole("group", { name: "Primary pool" }));
    await user.click(primary.getByRole("button", { name: "Test connection" }));
    expect(await primary.findByText("Pool connection successful")).toBeVisible();
    expect(primary.queryByLabelText("Worker suffix (optional)")).not.toBeInTheDocument();
    expect(primary.getByText("Worker name will be appended to this username when applied to miners.")).toBeVisible();
    expect(mocks.client.testConnection).toHaveBeenCalledWith({
      accountId: "customer",
      configurationId: configuration.configurationId,
      expectedVersion: configuration.version,
      pool: { slotId: "slot-1", url: "stratum+tcp://pool.example", username: "payout", workerTemplate: "{miner_name}" },
    });
    expect(mocks.client.reviewEdit).not.toHaveBeenCalled();
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
    await user.type(primary.getByLabelText("Pool URL"), ":3333");
    expect(primary.queryByText("Pool connection successful")).not.toBeInTheDocument();
    expect(primary.getByRole("button", { name: "Test connection" })).toBeDisabled();
    await user.click(primary.getByLabelText("Keep saved password"));
    mocks.client.testConnection.mockResolvedValue({ connected: false });
    await user.click(primary.getByRole("button", { name: "Test connection" }));
    expect(
      await primary.findByText("We couldn't connect with your pool. Review your pool details and try again."),
    ).toBeVisible();
    expect(mocks.client.testConnection.mock.calls[1][0].pool.password).toBe("");
  });

  it("ignores an older connection response and keeps backup results separate", async () => {
    const user = userEvent.setup();
    let finishOld: (result: { connected: boolean }) => void = () => {};
    mocks.client.testConnection.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishOld = resolve;
        }),
    );
    mocks.client.testConnection.mockResolvedValue({ connected: false });
    render(<ConfigurationEditor original={configuration} onSaved={vi.fn()} onDismiss={vi.fn()} />);
    const primary = within(screen.getByRole("group", { name: "Primary pool" }));
    await user.click(primary.getByRole("button", { name: "Test connection" }));
    await user.click(primary.getByLabelText("Keep saved password"));
    await user.type(primary.getByLabelText("Pool password (optional)"), "new-password");
    await user.click(primary.getByRole("button", { name: "Test connection" }));
    expect(
      await primary.findByText("We couldn't connect with your pool. Review your pool details and try again."),
    ).toBeVisible();
    finishOld({ connected: true });
    await waitFor(() => expect(primary.getByRole("status")).toHaveTextContent("We couldn't connect with your pool."));
    await user.click(screen.getByRole("button", { name: "Add backup pool" }));
    const backup = within(screen.getByRole("group", { name: "Backup 1" }));
    expect(backup.getByRole("status")).toBeEmptyDOMElement();
    expect(backup.getByRole("button", { name: "Test connection" })).toBeDisabled();
  });

  it("preserves hidden credentials and requires a user password before updating assigned equipment", async () => {
    const user = userEvent.setup();
    const saved = vi.fn();
    mocks.client.reviewEdit.mockResolvedValue({
      reviewId: "review-1",
      configuration,
      affectedMiners: 2n,
      affectedHashboards: 0n,
      equipment: [
        { kind: "device", targetId: 7n, label: "North miner" },
        { kind: "device", targetId: 12n, label: "South miner" },
      ],
    });
    render(<ConfigurationEditor original={configuration} onSaved={saved} onDismiss={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "Add backup pool" }));
    const backup = within(screen.getByRole("group", { name: "Backup 1" }));
    await user.type(backup.getByLabelText("Pool URL"), "stratum+tcp://backup.example");
    await user.type(backup.getByLabelText("Username"), "payout");
    await user.click(screen.getByRole("button", { name: "Review changes" }));
    expect(
      await screen.findByText("Saving this change will update 2 miners with this pool configuration applied."),
    ).toBeInTheDocument();
    expect(screen.getByText("Backup 1 URL")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Affected equipment (2)")).toBeVisible());
    expect(screen.getByText("North miner")).toBeVisible();
    expect(screen.getByText("South miner")).toBeVisible();
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
    expect(mocks.client.reviewEdit.mock.calls[0][0].configuration.pools[0]).not.toHaveProperty("password");
    const confirm = screen.getByRole("button", { name: "Save and update equipment" });
    expect(confirm).toBeDisabled();
    await user.type(screen.getByLabelText("Your password"), "current-user-password");
    await user.click(confirm);
    await waitFor(() =>
      expect(mocks.client.confirmReview).toHaveBeenCalledWith(
        {
          accountId: "customer",
          reviewId: "review-1",
          password: "current-user-password",
        },
        expect.anything(),
      ),
    );
    expect(saved).toHaveBeenCalledWith(configuration, "operation-1");
  });

  it("keeps the equipment selection when creating a configuration inline and does not apply on create", async () => {
    const user = userEvent.setup();
    const created = create(ConfigurationSchema, {
      ...configuration,
      configurationId: "new-config",
      name: "New pools",
      assignedCount: 0n,
    });
    mocks.client.createConfiguration.mockResolvedValue({ configuration: created });
    mocks.client.reviewAssignment.mockResolvedValue({
      reviewId: "apply-1",
      configuration: created,
      affectedMiners: 2n,
      affectedHashboards: 0n,
    });
    render(<ConfigurationSelector deviceIdentifiers={["miner-a", "miner-b"]} onDismiss={vi.fn()} />);
    await authenticateForPools(user);
    await user.click(screen.getByRole("button", { name: "New pool configuration" }));
    const editor = screen.getByRole("dialog", { name: "Create pool configuration" });
    await user.type(within(editor).getByLabelText("Name"), "New pools");
    await user.type(within(editor).getByLabelText("Pool URL"), "stratum+tcp://pool.example");
    await user.type(within(editor).getByLabelText("Username"), "payout");
    mocks.client.listConfigurations.mockResolvedValue({ configurations: [configuration, created], nextPageToken: "" });
    await user.click(within(editor).getByRole("button", { name: "Save configuration" }));
    await screen.findByRole("dialog", { name: "Edit pool" });
    expect(mocks.client.reviewAssignment).not.toHaveBeenCalled();
    expect(mocks.client.createConfiguration.mock.calls[0][0].configuration.pools[0].workerTemplate).toBe(
      "{miner_name}",
    );
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
    expect(screen.getByRole("radio", { name: "New pools" })).toBeChecked();
    expect(screen.getByRole("button", { name: "Assign" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Assign" }));
    await screen.findByRole("dialog", { name: /Assign pool configuration to/ });
    expect(mocks.client.reviewAssignment).toHaveBeenCalledWith(
      expect.objectContaining({
        accountId: "customer",
        configurationId: created.configurationId,
        expectedVersion: created.version,
        deviceIdentifiers: ["miner-a", "miner-b"],
        deviceIds: [],
      }),
      expect.objectContaining({ timeoutMs: expect.any(Number) }),
    );
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
  });

  it("shows whole-container scope and reviews that controller before applying", async () => {
    const user = userEvent.setup();
    const dismiss = vi.fn();
    render(
      <ConfigurationSelector
        deviceIdentifiers={["ct1"]}
        selectionDescription="Applies to the whole container."
        onDismiss={dismiss}
      />,
    );
    await authenticateForPools(user);
    await waitFor(() => expect(screen.getByText("Applies to the whole container.")).toBeVisible());
    expect(screen.queryByText("1 miners selected")).not.toBeInTheDocument();
    await user.click(await screen.findByRole("radio", { name: "Production" }));
    await user.click(screen.getByRole("button", { name: "Assign" }));
    await waitFor(() => expect(dismiss).toHaveBeenCalled());
    expect(mocks.client.reviewAssignment).toHaveBeenCalledWith(
      expect.objectContaining({ deviceIdentifiers: ["ct1"], boards: undefined }),
      expect.anything(),
    );
    expect(mocks.client.confirmReview).toHaveBeenCalledTimes(1);
  });

  it("allows applying without permission to manage configurations", async () => {
    mocks.permissions.manage = false;
    const user = userEvent.setup();
    render(<ConfigurationSelector deviceIdentifiers={["miner-a"]} onDismiss={vi.fn()} />);
    await authenticateForPools(user);
    await user.click(await screen.findByRole("radio", { name: "Production" }));
    expect(screen.queryByRole("button", { name: "New pool configuration" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Assign" })).toBeEnabled();
  });

  it("reviews hosted miner IDs in the selected account before assigning", async () => {
    mocks.permissions.manage = false;
    const user = userEvent.setup();
    const dismiss = vi.fn();
    render(<ConfigurationSelector deviceIds={[7n, 12n]} onDismiss={dismiss} />);
    await authenticateForPools(user);
    await user.click(await screen.findByRole("radio", { name: "Production" }));
    await user.click(screen.getByRole("button", { name: "Assign" }));
    const review = await screen.findByRole("dialog", { name: /Assign pool configuration to/ });
    expect(mocks.client.reviewAssignment).toHaveBeenCalledWith(
      expect.objectContaining({
        accountId: "customer",
        configurationId: configuration.configurationId,
        expectedVersion: configuration.version,
        deviceIdentifiers: [],
        deviceIds: [7n, 12n],
      }),
      expect.objectContaining({ timeoutMs: expect.any(Number) }),
    );
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
    expect(dismiss).not.toHaveBeenCalled();
    expect(within(review).queryByLabelText("Your password")).not.toBeInTheDocument();
    await user.click(within(review).getByRole("button", { name: "Assign" }));
    await waitFor(() =>
      expect(mocks.client.confirmReview).toHaveBeenCalledWith(
        {
          accountId: "customer",
          reviewId: "apply-1",
          password: "actor-password",
        },
        expect.objectContaining({ timeoutMs: 30_000 }),
      ),
    );
    expect(dismiss).toHaveBeenCalledOnce();
  });

  it("keeps the picker hidden after a failed login and allows retry", async () => {
    const user = userEvent.setup();
    mocks.verifyCredentials.mockRejectedValueOnce(new Error("invalid password"));
    render(<ConfigurationSelector deviceIds={[7n]} onDismiss={vi.fn()} />);
    expect(mocks.client.listConfigurations).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText("Username"), "pool-operator");
    await user.type(screen.getByLabelText("Password"), "wrong-password");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(await screen.findByText("Invalid credentials entered.")).toBeVisible();
    expect(screen.queryByRole("dialog", { name: "Edit pool" })).not.toBeInTheDocument();
    expect(mocks.client.listConfigurations).not.toHaveBeenCalled();
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
    await user.clear(screen.getByLabelText("Password"));
    await user.type(screen.getByLabelText("Password"), "actor-password");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("dialog", { name: "Edit pool" });
    expect(mocks.verifyCredentials).toHaveBeenLastCalledWith({ username: "pool-operator", password: "actor-password" });
  });

  it("cancels the login check without loading configurations or assigning equipment", async () => {
    const user = userEvent.setup();
    const dismiss = vi.fn();
    render(<ConfigurationSelector deviceIds={[7n]} onDismiss={dismiss} />);
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(dismiss).toHaveBeenCalledOnce();
    expect(mocks.client.listConfigurations).not.toHaveBeenCalled();
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
  });

  it.each(["", "rack-{miner_id}"])("preserves imported worker policy %s during name-only edits", (workerTemplate) => {
    const custom = create(ConfigurationSchema, {
      ...configuration,
      pools: [{ ...configuration.pools[0], workerTemplate }],
    });
    const pools = configurationDraft(custom);
    expect(poolInput(pools[0]).workerTemplate).toBe(workerTemplate);
    expect(poolInput(pools[0])).not.toHaveProperty("password");
    expect(configurationChanges(custom, "Renamed", pools)).toEqual([
      { field: "Name", before: custom.name, after: "Renamed" },
    ]);
    expect(configurationChanges(configuration, configuration.name, configurationDraft(configuration))).toEqual([]);
  });

  it("makes password clearing explicit without putting secrets in change descriptions", () => {
    const pools = configurationDraft(configuration);
    expect(poolInput(pools[0])).not.toHaveProperty("password");
    pools[0].preservePassword = false;
    expect(poolInput(pools[0]).password).toBe("");
    expect(configurationChanges(configuration, configuration.name, pools)).toContainEqual({
      field: "Primary pool password",
      before: "Saved password",
      after: "No password",
    });
    pools[0].password = "private-replacement";
    expect(JSON.stringify(configurationChanges(configuration, configuration.name, pools))).not.toContain(
      "private-replacement",
    );
  });
});

it("assigns a single module directly even when it expands to multiple hashboards", async () => {
  const user = userEvent.setup();
  mocks.client.getAssignments.mockResolvedValue({
    selectedCount: 2n,
    assignments: [
      { configurationId: configuration.configurationId },
      { configurationId: configuration.configurationId },
    ],
  });
  mocks.client.reviewAssignment.mockResolvedValue({
    reviewId: "module-review",
    configuration,
    affectedMiners: 0n,
    affectedHashboards: 2n,
    equipment: [],
  });
  render(<ConfigurationSelector boards={{ moduleIds: [42n] }} onDismiss={vi.fn()} />);
  await authenticateForPools(user);
  expect(await screen.findByRole("radio", { name: /Production/ })).toBeChecked();
  await user.click(screen.getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(mocks.client.confirmReview).toHaveBeenCalledOnce());
  expect(mocks.client.reviewAssignment).toHaveBeenCalledWith(
    expect.objectContaining({ boards: { moduleIds: [42n] }, configurationId: configuration.configurationId }),
    expect.objectContaining({ timeoutMs: expect.any(Number) }),
  );
  expect(screen.queryByRole("dialog", { name: /Assign pool configuration to/ })).not.toBeInTheDocument();
});

it("confirms mixed equipment with concise counts before assigning", async () => {
  const user = userEvent.setup();
  const dismiss = vi.fn();
  const applied = vi.fn();
  mocks.client.reviewAssignment.mockResolvedValue({
    reviewId: "boards-review",
    configuration,
    affectedMiners: 1n,
    affectedHashboards: 2n,
    equipment: [
      { kind: "device", targetId: 7n, label: "North miner" },
      { kind: "hashboard_installation", targetId: 101n, label: "Container A / Module 2 / Slot 1" },
      { kind: "hashboard_installation", targetId: 103n, label: "Container A / Module 2 / Slot 3" },
    ],
  });
  render(
    <ConfigurationSelector
      deviceIds={[7n]}
      boards={{ hashboardIds: [101n, 103n] }}
      onDismiss={dismiss}
      onApplied={applied}
    />,
  );
  await authenticateForPools(user);
  await user.click(await screen.findByRole("radio", { name: "Production" }));
  await user.click(screen.getByRole("button", { name: "Assign" }));
  const review = within(await screen.findByRole("dialog", { name: /Assign pool configuration to/ }));
  await waitFor(() =>
    expect(
      screen.getByRole("dialog", { name: "Assign pool configuration to 1 miner and 2 hashboards?" }),
    ).toBeVisible(),
  );
  expect(review.getByText("Assign Production? Pool changes may briefly interrupt mining.")).toBeVisible();
  expect(review.queryByRole("region", { name: "Affected equipment" })).not.toBeInTheDocument();
  expect(mocks.client.reviewAssignment).toHaveBeenCalledWith(
    expect.objectContaining({ deviceIds: [7n], boards: { hashboardIds: [101n, 103n] } }),
    expect.objectContaining({ timeoutMs: expect.any(Number) }),
  );
  expect(mocks.client.confirmReview).not.toHaveBeenCalled();
  expect(dismiss).not.toHaveBeenCalled();
  expect(review.queryByLabelText("Your password")).not.toBeInTheDocument();
  expect(review.getByRole("button", { name: "Assign" })).toBeEnabled();
  await user.click(review.getByRole("button", { name: "Assign" }));
  await waitFor(() =>
    expect(mocks.client.confirmReview).toHaveBeenCalledWith(
      {
        accountId: "customer",
        reviewId: "boards-review",
        password: "actor-password",
      },
      expect.objectContaining({ timeoutMs: 30_000 }),
    ),
  );
  expect(applied).toHaveBeenCalledWith("operation-1");
  expect(dismiss).toHaveBeenCalledOnce();
});

it.each([
  new ConnectError(
    "Selected equipment is in maintenance. Remove it from maintenance and try again.",
    Code.FailedPrecondition,
  ),
  new ConnectError("Fleet could not load the selected equipment.", Code.Unavailable),
])("keeps review failures visible without confirming or dismissing the selection: %s", async (error) => {
  const user = userEvent.setup();
  const dismiss = vi.fn();
  mocks.client.reviewAssignment.mockRejectedValue(error);
  render(<ConfigurationSelector deviceIds={[7n]} onDismiss={dismiss} />);
  await authenticateForPools(user);
  await user.click(await screen.findByRole("radio", { name: "Production" }));
  await user.click(screen.getByRole("button", { name: "Assign" }));
  expect(await screen.findByText(error.rawMessage)).toBeVisible();
  expect(screen.getByRole("dialog", { name: "Edit pool" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Assign" })).toBeEnabled();
  expect(screen.queryByRole("dialog", { name: /Assign pool configuration to/ })).not.toBeInTheDocument();
  expect(mocks.client.confirmReview).not.toHaveBeenCalled();
  expect(dismiss).not.toHaveBeenCalled();
});

it.each([{ deviceIdentifiers: ["miner-a"] }, { deviceIds: [7n] }, { boards: { hashboardIds: [101n] } }])(
  "assigns a single selected unit directly: case %#",
  async (selection) => {
    const user = userEvent.setup();
    const dismiss = vi.fn();
    const applied = vi.fn();
    render(<ConfigurationSelector {...selection} onDismiss={dismiss} onApplied={applied} />);
    await authenticateForPools(user);
    await user.click(await screen.findByRole("radio", { name: "Production" }));
    await user.click(screen.getByRole("button", { name: "Assign" }));
    await waitFor(() => expect(applied).toHaveBeenCalledWith("operation-1"));
    expect(mocks.client.confirmReview).toHaveBeenCalledExactlyOnceWith(
      {
        accountId: "customer",
        reviewId: "apply-1",
        password: "actor-password",
      },
      expect.objectContaining({ timeoutMs: 30_000 }),
    );
    expect(mocks.track).toHaveBeenCalledWith(mocks.client, "customer", configuration, "operation-1", [], undefined);
    expect(dismiss).toHaveBeenCalledOnce();
    expect(screen.queryByRole("dialog", { name: /Assign pool configuration to/ })).not.toBeInTheDocument();
  },
);

it("requires confirmation for filtered selections even with one reported miner and allows cancellation", async () => {
  const user = userEvent.setup();
  mocks.client.reviewAssignment.mockResolvedValue({
    reviewId: "filter-review",
    configuration,
    affectedMiners: 1n,
    affectedHashboards: 0n,
    equipment: [],
  });
  render(<ConfigurationSelector filter={create(MinerListFilterSchema)} selectedCount={1} onDismiss={vi.fn()} />);
  await authenticateForPools(user);
  await user.click(await screen.findByRole("radio", { name: "Production" }));
  await user.click(screen.getByRole("button", { name: "Assign" }));
  const dialog = await screen.findByRole("dialog", { name: "Assign pool configuration to 1 miner?" });
  expect(mocks.client.confirmReview).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await screen.findByRole("dialog", { name: "Edit pool" });
  expect(mocks.client.confirmReview).not.toHaveBeenCalled();
});

it.each(["unmount", "session change", "account change"])(
  "does not auto-confirm a late review after %s",
  async (action) => {
    const user = userEvent.setup();
    let resolveReview!: (value: unknown) => void;
    mocks.client.reviewAssignment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveReview = resolve;
        }),
    );
    const { unmount, rerender } = render(<ConfigurationSelector deviceIds={[7n]} onDismiss={vi.fn()} />);
    await authenticateForPools(user);
    await user.click(await screen.findByRole("radio", { name: "Production" }));
    await user.click(screen.getByRole("button", { name: "Assign" }));
    if (action === "unmount") unmount();
    else if (action === "account change") {
      mocks.accountId = "other-account";
      rerender(<ConfigurationSelector deviceIds={[7n]} onDismiss={vi.fn()} />);
    } else
      act(() =>
        useFleetStore.setState(({ auth }) => ({ auth: { ...auth, sessionGeneration: auth.sessionGeneration + 1 } })),
      );
    await act(async () =>
      resolveReview({
        reviewId: "late-review",
        configuration,
        affectedMiners: 1n,
        affectedHashboards: 0n,
        equipment: [],
      }),
    );
    expect(mocks.client.confirmReview).not.toHaveBeenCalled();
  },
);

it("reuses the canonical review when retrying an uncertain confirmation", async () => {
  const user = userEvent.setup();
  mocks.client.confirmReview.mockRejectedValueOnce(new ConnectError("Response lost", Code.Unavailable));
  render(<ConfigurationSelector deviceIds={[7n]} onDismiss={vi.fn()} />);
  await authenticateForPools(user);
  await user.click(await screen.findByRole("radio", { name: "Production" }));
  await user.click(screen.getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(screen.getByText(/Fleet couldn’t confirm whether the update was accepted/)).toBeVisible());
  await user.click(screen.getByRole("button", { name: "Retry confirmation" }));
  await waitFor(() => expect(mocks.client.confirmReview).toHaveBeenCalledTimes(2));
  expect(mocks.client.reviewAssignment).toHaveBeenCalledOnce();
  expect(mocks.client.confirmReview.mock.calls.map(([request]) => request.reviewId)).toEqual(["apply-1", "apply-1"]);
});

it("refreshes a definitively rejected review before retrying a single assignment", async () => {
  const user = userEvent.setup();
  mocks.client.confirmReview.mockRejectedValueOnce(new ConnectError("Review expired", Code.FailedPrecondition));
  render(<ConfigurationSelector deviceIds={[7n]} onDismiss={vi.fn()} />);
  await authenticateForPools(user);
  await user.click(await screen.findByRole("radio", { name: "Production" }));
  await user.click(screen.getByRole("button", { name: "Assign" }));
  expect(await screen.findByText("Review expired")).toBeVisible();
  mocks.client.reviewAssignment.mockResolvedValue({
    reviewId: "fresh-review",
    configuration,
    affectedMiners: 1n,
    affectedHashboards: 0n,
    equipment: [],
  });
  await user.click(screen.getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(mocks.client.confirmReview).toHaveBeenCalledTimes(2));
  expect(mocks.client.reviewAssignment).toHaveBeenCalledTimes(2);
  expect(mocks.client.confirmReview.mock.calls[1][0].reviewId).toBe("fresh-review");
});

it("does not duplicate a direct assignment while confirmation is pending", async () => {
  const user = userEvent.setup();
  let resolveConfirmation!: (value: unknown) => void;
  mocks.client.confirmReview.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveConfirmation = resolve;
      }),
  );
  const dismiss = vi.fn();
  render(<ConfigurationSelector deviceIds={[7n]} onDismiss={dismiss} />);
  await authenticateForPools(user);
  await user.click(await screen.findByRole("radio", { name: "Production" }));
  await user.dblClick(screen.getByRole("button", { name: "Assign" }));
  expect(mocks.client.confirmReview).toHaveBeenCalledOnce();
  expect(mocks.client.reviewAssignment).toHaveBeenCalledOnce();
  expect(dismiss).not.toHaveBeenCalled();
  await act(async () => resolveConfirmation({ configuration, operationId: "operation-1" }));
  expect(dismiss).toHaveBeenCalledOnce();
});

it.each([[7n], [7n, 8n]])(
  "retains an initial assignment review across dismissal and reload: case %#",
  async (...deviceIds) => {
    const user = userEvent.setup();
    const dismiss = vi.fn();
    mocks.client.confirmReview.mockRejectedValueOnce(new ConnectError("Reply lost", Code.Unavailable));
    const view = render(<ConfigurationSelector deviceIds={deviceIds} onDismiss={dismiss} />);
    await authenticateForPools(user);
    await user.click(await screen.findByRole("radio", { name: "Production" }));
    await user.click(screen.getByRole("button", { name: "Assign" }));
    if (deviceIds.length > 1)
      await user.click(
        within(await screen.findByRole("dialog", { name: /Assign pool configuration to/ })).getByRole("button", {
          name: "Assign",
        }),
      );
    await waitFor(() => expect(mocks.client.confirmReview).toHaveBeenCalledOnce());
    expect(useConfirmationRecovery.getState().attempts).toEqual([
      expect.objectContaining({ reviewId: "apply-1", status: "unknown" }),
    ]);
    expect(mocks.client.confirmReview.mock.calls[0][1]).toEqual(expect.objectContaining({ timeoutMs: 30_000 }));
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(dismiss).toHaveBeenCalledOnce();
    view.unmount();
    await useConfirmationRecovery.persist.rehydrate();
    expect(useConfirmationRecovery.getState().attempts).toEqual([
      expect.objectContaining({ reviewId: "apply-1", status: "unknown" }),
    ]);
    expect(sessionStorage.getItem("proto-pool-confirmation-recovery")).not.toContain("actor-password");
    expect(mocks.client.confirmReview).toHaveBeenCalledOnce();
  },
);

it.each(["unmount", "session change", "account change"])(
  "preserves an initial confirmation and ignores its late response after %s",
  async (action) => {
    const user = userEvent.setup();
    let resolveConfirmation!: (value: unknown) => void;
    mocks.client.confirmReview.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveConfirmation = resolve;
        }),
    );
    const applied = vi.fn();
    const dismiss = vi.fn();
    const selector = <ConfigurationSelector deviceIds={[7n]} onDismiss={dismiss} onApplied={applied} />;
    const view = render(selector);
    await authenticateForPools(user);
    await user.click(await screen.findByRole("radio", { name: "Production" }));
    await user.click(screen.getByRole("button", { name: "Assign" }));
    expect(useConfirmationRecovery.getState().attempts[0].status).toBe("submitting");
    if (action === "unmount") view.unmount();
    else if (action === "account change") {
      mocks.accountId = "other-account";
      view.rerender(<ConfigurationSelector deviceIds={[7n]} onDismiss={dismiss} onApplied={applied} />);
    } else
      act(() =>
        useFleetStore.setState(({ auth }) => ({ auth: { ...auth, sessionGeneration: auth.sessionGeneration + 1 } })),
      );
    await act(async () => resolveConfirmation({ configuration, operationId: "operation-1" }));
    expect(applied).not.toHaveBeenCalled();
    expect(dismiss).not.toHaveBeenCalled();
    expect(mocks.track).not.toHaveBeenCalled();
    expect(useConfirmationRecovery.getState().attempts).toEqual([
      expect.objectContaining({ reviewId: "apply-1", status: "unknown" }),
    ]);
  },
);

// Library-only edits still use review + confirmation and must not outlive their editor.
it.each(["unmount", "session"])("does not confirm a delayed library edit after %s", async (end) => {
  let finish!: (value: unknown) => void;
  mocks.client.reviewEdit.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const onSaved = vi.fn();
  const onDismiss = vi.fn();
  const view = render(
    <ConfigurationEditor original={{ ...configuration, assignedCount: 0n }} onSaved={onSaved} onDismiss={onDismiss} />,
  );
  await userEvent.setup().click(screen.getByRole("button", { name: "Save configuration" }));
  expect(mocks.client.reviewEdit).toHaveBeenCalledOnce();
  if (end === "unmount") view.unmount();
  else
    act(() =>
      useFleetStore.setState((state) => ({
        auth: { ...state.auth, sessionGeneration: state.auth.sessionGeneration + 1 },
      })),
    );
  await act(async () => finish({ reviewId: "late-edit", affectedMiners: 0n, affectedHashboards: 0n }));
  expect(mocks.client.confirmReview).not.toHaveBeenCalled();
  expect(onSaved).not.toHaveBeenCalled();
  expect(onDismiss).not.toHaveBeenCalled();
});

it.each(["unmount", "session"])("does not deliver a completed library edit into an ended editor: %s", async (end) => {
  let finish!: (value: unknown) => void;
  mocks.client.reviewEdit.mockResolvedValueOnce({ reviewId: "edit", affectedMiners: 0n, affectedHashboards: 0n });
  mocks.client.confirmReview.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const onSaved = vi.fn();
  const onDismiss = vi.fn();
  const view = render(
    <ConfigurationEditor original={{ ...configuration, assignedCount: 0n }} onSaved={onSaved} onDismiss={onDismiss} />,
  );
  await userEvent.setup().click(screen.getByRole("button", { name: "Save configuration" }));
  expect(mocks.client.confirmReview).toHaveBeenCalledOnce();
  if (end === "unmount") view.unmount();
  else
    act(() =>
      useFleetStore.setState((state) => ({
        auth: { ...state.auth, sessionGeneration: state.auth.sessionGeneration + 1 },
      })),
    );
  await act(async () => finish({ configuration, operationId: "" }));
  expect(onSaved).not.toHaveBeenCalled();
  expect(onDismiss).not.toHaveBeenCalled();
});
