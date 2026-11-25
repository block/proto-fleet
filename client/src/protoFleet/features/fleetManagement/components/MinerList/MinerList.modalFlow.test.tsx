import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";
import MinerList from "./MinerList";
import { ComponentType, ErrorMessageSchema } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { ConfigurationSchema } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { type AccountScope, AccountScopeContext } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { useConfirmationRecovery } from "@/protoFleet/features/poolConfigurations/confirmationAttempts";
import type { ColConfig } from "@/shared/components/List/types";

const mocks = vi.hoisted(() => ({
  permissions: ["miner:update_pools"],
  errorsByDevice: {},
  verifyCredentials: vi.fn(),
  trackPoolOperation: vi.fn(),
  client: { listConfigurations: vi.fn(), getAssignments: vi.fn(), reviewAssignment: vi.fn(), confirmReview: vi.fn() },
}));
let miner = create(MinerStateSnapshotSchema);

vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useUsername: () => "",
  useHasPermission: () => false,
  usePermissions: () => mocks.permissions,
}));
vi.mock("@/protoFleet/api/clients", () => ({ authClient: { verifyCredentials: mocks.verifyCredentials } }));
vi.mock("@/protoFleet/features/poolConfigurations/assignmentProgress", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/features/poolConfigurations/assignmentProgress")>()),
  trackPoolOperation: mocks.trackPoolOperation,
}));
vi.mock("@/protoFleet/features/poolConfigurations/api", async (importOriginal) => {
  const { useAccountScope } = await import("@/protoFleet/features/hostedAccounts/AccountScope");
  return {
    ...(await importOriginal<typeof import("@/protoFleet/features/poolConfigurations/api")>()),
    usePoolConfigurationClient: () => {
      const scope = useAccountScope();
      return { client: mocks.client, accountId: scope?.account.accountId ?? "", readOnly: scope?.readOnly ?? false };
    },
  };
});
vi.mock("@/protoFleet/api/useDeviceErrors", () => ({
  useDeviceErrors: () => ({ errorsByDevice: mocks.errorsByDevice, refetch: vi.fn() }),
}));
vi.mock("@/protoFleet/api/useRefreshMiners", () => ({
  default: () => ({ refreshMiners: vi.fn() }),
}));
vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: () => ({ startMining: vi.fn() }),
}));
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu", () => ({
  default: () => null,
}));
// Keep the actual column renderers, click targets, status modal, and assignment flow.
vi.mock("@/shared/components/List", () => ({
  default: ({
    items,
    colConfig,
  }: {
    items: { selectionKey: string }[];
    colConfig: ColConfig<{ selectionKey: string }, string, string>;
  }) => (
    <div>
      {items.map((item) => (
        <div key={item.selectionKey}>
          {colConfig.name?.component?.(item, [])}
          {colConfig.status?.component?.(item, [])}
        </div>
      ))}
    </div>
  ),
}));
vi.mock("@/protoFleet/features/auth/components/AuthenticateMiners", () => ({
  AuthenticateMiners: ({ onClose }: { onClose: () => void }) => (
    <div data-testid="authenticate-miners">
      <button onClick={onClose}>Close authentication</button>
    </div>
  ),
}));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => ({ isPhone: false }),
}));
vi.mock("@/shared/hooks/useReactiveLocalStorage", () => ({
  useReactiveLocalStorage: () => [false],
}));

const minerListContent = (scope: AccountScope | null = null) => (
  <MemoryRouter>
    <AccountScopeContext.Provider value={scope}>
      <MinerList
        title="Miners"
        minerIds={[miner.deviceIdentifier]}
        miners={{ [miner.deviceIdentifier]: miner }}
        errorsByDevice={mocks.errorsByDevice}
        errorsLoaded
        getActiveBatches={() => []}
        totalMiners={1}
        onAddMiners={vi.fn()}
      />
    </AccountScopeContext.Provider>
  </MemoryRouter>
);
const renderMinerList = (scope: AccountScope | null = null) => render(minerListContent(scope));

const accountScope = (capabilities: bigint, readOnly = false): AccountScope => ({
  account: create(AccountSchema, { accountId: "customer", effectiveCapabilities: capabilities }),
  readOnly,
  client: {} as AccountScope["client"],
  signal: new AbortController().signal,
  refreshAccount: vi.fn(),
  invalidateAccount: vi.fn(),
});

const configuration = create(ConfigurationSchema, { configurationId: "config-1", name: "Production", version: 3n });

describe("MinerList modal flow orchestration", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useConfirmationRecovery.setState({ attempts: [] });
    vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue(
      Object.assign([new DOMRect()], { item: () => null }),
    );
    mocks.permissions = ["miner:update_pools"];
    mocks.errorsByDevice = {};
    mocks.verifyCredentials.mockResolvedValue({});
    mocks.client.listConfigurations.mockResolvedValue({ configurations: [configuration], nextPageToken: "" });
    mocks.client.getAssignments.mockResolvedValue({ assignments: [], selectedCount: 1n });
    mocks.client.confirmReview.mockReset().mockResolvedValue({ configuration, operationId: "operation-1" });
    mocks.client.reviewAssignment.mockResolvedValue({
      reviewId: "review-1",
      configuration,
      affectedMiners: 1n,
      affectedHashboards: 0n,
      equipment: [],
    });
    miner = create(MinerStateSnapshotSchema, {
      deviceIdentifier: "miner-1",
      name: "Miner 1",
      pairingStatus: PairingStatus.PAIRED,
      deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
    });
  });

  afterEach(() => vi.restoreAllMocks());

  it.each(["Needs attention", "View issues"])("explains Pool required when clicking %s", async (target) => {
    const user = userEvent.setup();
    renderMinerList();
    await user.click(screen.getByRole("button", { name: target }));
    await waitFor(() => expect(screen.getByText("Pool required")).toBeVisible());
    await waitFor(() =>
      expect(screen.getByText("This miner needs a pool configuration to start mining.")).toBeVisible(),
    );
    await waitFor(() => expect(screen.getByRole("button", { name: "Assign pool configuration" })).toBeVisible());
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(mocks.verifyCredentials).not.toHaveBeenCalled();
    expect(mocks.client.listConfigurations).not.toHaveBeenCalled();
  });

  it("keeps hardware issues visible alongside Pool required", async () => {
    mocks.errorsByDevice = {
      "miner-1": [
        create(ErrorMessageSchema, {
          componentType: ComponentType.FAN,
          componentId: "1",
          summary: "Fan speed too low",
        }),
      ],
    };
    renderMinerList();
    await userEvent.click(screen.getByRole("button", { name: "Needs attention" }));
    await waitFor(() => expect(screen.getByText("Pool required")).toBeVisible());
    await waitFor(() => expect(screen.getByText("Fan speed too low")).toBeVisible());
  });

  it.each(["operator without pool permission", "account without pool capability", "read-only account"])(
    "shows the explanation without an assignment action for %s",
    async (scenario) => {
      mocks.permissions = scenario === "operator without pool permission" ? [] : ["miner:update_pools"];
      const scope =
        scenario === "operator without pool permission"
          ? null
          : accountScope(scenario === "read-only account" ? 5n : 1n, scenario === "read-only account");
      renderMinerList(scope);
      await userEvent.click(screen.getByRole("button", { name: "Needs attention" }));
      await waitFor(() => expect(screen.getByText("Pool required")).toBeVisible());
      await waitFor(() =>
        expect(screen.getByText("This miner needs a pool configuration to start mining.")).toBeVisible(),
      );
      expect(screen.queryByRole("button", { name: "Assign pool configuration" })).not.toBeInTheDocument();
      expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    },
  );

  it("verifies credentials only after the action and confirms assignment for the same miner in its account", async () => {
    const user = userEvent.setup();
    mocks.permissions = [];
    renderMinerList(accountScope(5n));
    await user.click(screen.getByRole("button", { name: "View issues" }));
    await user.click(screen.getByRole("button", { name: "Assign pool configuration" }));
    expect(screen.queryByText("Pool required")).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Log in to update your pool settings")).toBeVisible());
    expect(mocks.client.listConfigurations).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText("Username"), "pool-operator");
    await user.type(screen.getByLabelText("Password"), "actor-password");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    const picker = await screen.findByRole("dialog", { name: "Edit pool" });
    expect(mocks.verifyCredentials).toHaveBeenCalledWith({ username: "pool-operator", password: "actor-password" });
    await user.click(within(picker).getByRole("radio", { name: "Production" }));
    await user.click(within(picker).getByRole("button", { name: "Assign" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(mocks.client.reviewAssignment).toHaveBeenCalledWith(
      expect.objectContaining({
        accountId: "customer",
        configurationId: "config-1",
        deviceIdentifiers: ["miner-1"],
        deviceIds: [],
      }),
      expect.anything(),
    );
    expect(mocks.client.confirmReview).toHaveBeenCalledExactlyOnceWith(
      { accountId: "customer", reviewId: "review-1", password: "actor-password" },
      expect.objectContaining({ timeoutMs: 30_000 }),
    );
    expect(mocks.trackPoolOperation).toHaveBeenCalledWith(
      mocks.client,
      "customer",
      configuration,
      "operation-1",
      [],
      expect.any(AbortSignal),
    );
    expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  });

  it("retries an explicitly uncertain assignment with the same review and dismisses after confirmation", async () => {
    const user = userEvent.setup();
    mocks.client.confirmReview.mockRejectedValueOnce(new ConnectError("Response lost", Code.Unavailable));
    renderMinerList();
    await user.click(screen.getByRole("button", { name: "View issues" }));
    await user.click(screen.getByRole("button", { name: "Assign pool configuration" }));
    await user.type(screen.getByLabelText("Username"), "pool-operator");
    await user.type(screen.getByLabelText("Password"), "actor-password");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    const picker = await screen.findByRole("dialog", { name: "Edit pool" });
    await user.click(within(picker).getByRole("radio", { name: "Production" }));
    await user.click(within(picker).getByRole("button", { name: "Assign" }));
    const recovery = await screen.findByRole("dialog", { name: "Assign pool configuration to 1 miner?" });
    expect(mocks.client.confirmReview).toHaveBeenCalledOnce();
    expect(mocks.trackPoolOperation).not.toHaveBeenCalled();
    expect(useConfirmationRecovery.getState().attempts).toEqual([
      expect.objectContaining({ reviewId: "review-1", status: "unknown" }),
    ]);
    await user.click(within(recovery).getByRole("button", { name: "Retry confirmation" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(mocks.client.reviewAssignment).toHaveBeenCalledOnce();
    expect(mocks.client.confirmReview).toHaveBeenCalledTimes(2);
    for (const [request] of mocks.client.confirmReview.mock.calls) {
      expect(request).toEqual({ accountId: "", reviewId: "review-1", password: "actor-password" });
    }
    expect(mocks.trackPoolOperation).toHaveBeenCalledOnce();
    expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  });

  it("supports keyboard activation and dismissing credentials back to the list", async () => {
    const user = userEvent.setup();
    renderMinerList();
    await user.tab();
    expect(screen.getByRole("button", { name: "View issues" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("button", { name: "Close dialog" })).toHaveFocus();
    await user.tab({ shift: true });
    expect(screen.getByRole("button", { name: "Assign pool configuration" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(screen.getByLabelText("Username")).toHaveFocus();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Needs attention" }));
    await waitFor(() => expect(screen.getByText("Pool required")).toBeVisible());
  });

  it("keeps authentication remediation for miners that need pairing authentication", async () => {
    miner.pairingStatus = PairingStatus.AUTHENTICATION_NEEDED;
    miner.deviceStatus = DeviceStatus.ONLINE;
    renderMinerList();
    await userEvent.click(screen.getByRole("button", { name: "Needs attention" }));
    expect(screen.getByTestId("authenticate-miners")).toBeInTheDocument();
  });

  it("opens bulk authentication from the combined status and returns to the list", async () => {
    const user = userEvent.setup();
    miner.pairingStatus = PairingStatus.AUTHENTICATION_NEEDED;
    const view = renderMinerList();
    await user.click(screen.getByRole("button", { name: "Needs attention" }));
    expect(screen.getByText("Pool required")).toBeInTheDocument();
    expect(screen.getByText("Authentication needed")).toBeInTheDocument();
    expect(screen.queryByTestId("authenticate-miners")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    const assign = screen.getByRole("button", { name: "Assign pool configuration" });
    expect(assign).toBeDisabled();
    await user.click(assign);
    expect(screen.getByText("Authentication needed")).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Authenticate miner" }));
    expect(screen.getByTestId("authenticate-miners")).toBeInTheDocument();
    expect(screen.queryByText("Pool required")).not.toBeInTheDocument();
    expect(mocks.verifyCredentials).not.toHaveBeenCalled();
    expect(mocks.client.listConfigurations).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Close authentication" }));
    expect(screen.queryByTestId("authenticate-miners")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.client.listConfigurations).not.toHaveBeenCalled();

    miner = { ...miner, pairingStatus: PairingStatus.PAIRED };
    view.rerender(minerListContent());
    await user.click(screen.getByRole("button", { name: "View issues" }));
    expect(screen.getByText("Pool required")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Assign pool configuration" })).toBeVisible());
    expect(screen.getByRole("button", { name: "Assign pool configuration" })).toBeEnabled();
    expect(screen.queryByText("Authentication needed")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Authenticate miner" })).not.toBeInTheDocument();
  });

  it("keeps authentication available without pool-change permission", async () => {
    mocks.permissions = [];
    miner.pairingStatus = PairingStatus.AUTHENTICATION_NEEDED;
    renderMinerList();
    await userEvent.click(screen.getByRole("button", { name: "Needs attention" }));
    expect(screen.queryByRole("button", { name: "Assign pool configuration" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Authenticate miner" }));
    expect(screen.getByTestId("authenticate-miners")).toBeInTheDocument();
  });

  it("opens the status modal for ordinary miners and closes cleanly", async () => {
    miner.deviceStatus = DeviceStatus.ONLINE;
    renderMinerList();
    await userEvent.click(screen.getByRole("button", { name: "Hashing" }));
    await waitFor(() => expect(screen.getByText("All systems are operational")).toBeVisible());
    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("All systems are operational")).not.toBeInTheDocument();
  });
});
