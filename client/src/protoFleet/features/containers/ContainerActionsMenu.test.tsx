import { MemoryRouter, useLocation } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import ContainerActionsMenu from "./ContainerActionsMenu";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { STATUSES } from "@/shared/features/toaster";

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/FirmwareUpdateModal", () => ({
  default: ({ onConfirm }: { onConfirm: (id: string) => void }) => (
    <button onClick={() => onConfirm("ct1-firmware")}>Confirm hashboard update</button>
  ),
}));

const state = vi.hoisted(() => ({
  permissions: [] as string[],
  deleteMiners: vi.fn(),
  pushToast: vi.fn(),
  getState: vi.fn(),
  request: vi.fn(),
  statusRequest: vi.fn(),
  commandResult: vi.fn(),
  firmwareUpdate: vi.fn(),
}));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  usePermissions: () => state.permissions,
}));
vi.mock("@/protoFleet/api/clients", () => ({
  minerCommandClient: { firmwareUpdate: state.firmwareUpdate },
  fleetManagementClient: { deleteMiners: state.deleteMiners },
  miningGroupsClient: { getState: state.getState },
  containersClient: {
    getMiningStatus: state.statusRequest,
    getPowerTarget: state.request,
    setMining: state.request,
    setPowerTarget: state.request,
    getEquipmentCommandResult: state.commandResult,
  },
}));

vi.mock("@/protoFleet/features/poolConfigurations/ConfigurationSelector", () => ({
  ConfigurationSelector: ({
    deviceIdentifiers,
    selectionDescription,
    removalOnly,
    onDismiss,
  }: {
    deviceIdentifiers: string[];
    selectionDescription?: string;
    removalOnly?: boolean;
    onDismiss: () => void;
  }) => (
    <div role="dialog" aria-label="Pool configuration">
      <p>{deviceIdentifiers.join(",")}</p>
      <p>{selectionDescription}</p>
      {removalOnly ? <p>Remove assignment only</p> : null}
      <button onClick={onDismiss}>Cancel pool edit</button>
    </div>
  ),
}));

vi.mock("@/shared/features/toaster", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/features/toaster")>()),
  pushToast: state.pushToast,
}));

// JSDOM has no layout; keep the real menu and portal, bypassing only positioning.
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));

const container = create(ContainerSchema, {
  deviceIdentifier: "ct1/board",
  name: "Container 1",
});
const request = state.request;
const statusRequest = state.statusRequest;
const json = (data: Record<string, unknown>) => ({ batchIdentifier: "batch", ...data });
const openAction = async (name: string) => {
  fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
  const button = await screen.findByRole("button", { name });
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
};
const LocationProbe = () => <output data-testid="location">{useLocation().pathname}</output>;
const renderMenu = (overrides = {}, onUnpaired = vi.fn()) =>
  render(
    <MemoryRouter>
      <ContainerActionsMenu container={{ ...container, ...overrides }} onUnpaired={onUnpaired} />
      <LocationProbe />
    </MemoryRouter>,
  );

beforeEach(() => {
  state.pushToast.mockClear();
  state.getState.mockReset().mockResolvedValue({ enabled: false });
  state.permissions = [
    "miner:read",
    "miner:delete",
    "miner:stop_mining",
    "miner:start_mining",
    "miner:set_power_target",
    "miner:set_cooling_mode",
    "miner:blink_led",
    "miner:firmware_update",
    "miner:reboot",
    "miner:update_pools",
    "pool:read",
    "miner:update_worker_names",
  ];
  request.mockReset().mockResolvedValue(json({}));
  state.deleteMiners.mockReset().mockResolvedValue({ deletedCount: 1 });
  statusRequest.mockReset().mockImplementation(async () => json({ mining: true }));
  state.commandResult
    .mockReset()
    .mockResolvedValue({ result: { status: "finished", successCount: 1, failureCount: 0, deviceResults: [] } });
  vi.stubGlobal(
    "fetch",
    vi.fn(() => {
      throw new Error("container menu must use Fleet commands");
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

describe("container command dispatch", () => {
  it("offers site assignment with a scoped capability and no org-wide grant", () => {
    state.permissions = [];
    renderMenu({ canAssignSite: true });
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(screen.getByRole("button", { name: "Change site" })).toBeEnabled();
    expect(statusRequest).not.toHaveBeenCalled();
  });

  it("hides site assignment when the current site denies an org-wide manager", () => {
    state.permissions = ["miner:read", "site:manage"];
    renderMenu({ canAssignSite: false });
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(screen.queryByRole("button", { name: "Change site" })).not.toBeInTheDocument();
  });

  it.each([
    ["Sleep container", "mining/stop"],
    ["Wake container", "mining/start"],
  ])("confirms %s and dispatches once to the controller", async (label, path) => {
    statusRequest.mockResolvedValueOnce(json({ mining: path === "mining/stop" }));
    renderMenu();
    await openAction(label);
    expect(request).not.toHaveBeenCalled();
    const dialog = screen.getByTestId("container-action-dialog");
    expect(dialog).toHaveTextContent("Container 1");
    expect(dialog).toHaveTextContent(
      "Applies to the whole container. Isolated boards keep their individual mining state.",
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledWith(
      { deviceIdentifier: "ct1/board", mining: path === "mining/start" },
      expect.objectContaining({ timeoutMs: 10_000 }),
    );
  });

  it("cancels without issuing a command", async () => {
    renderMenu();
    await openAction("Sleep container");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(request).not.toHaveBeenCalled();
  });

  it.each([
    {
      source: "controller",
      body: { message: "Controller unavailable" },
      status: 503,
      detail: "Controller unavailable",
    },
    { source: "command authorization", body: { error: "Permission denied" }, status: 403, detail: "Permission denied" },
    {
      source: "node connection",
      body: { error: "Failed to connect to container" },
      status: 502,
      detail: "Failed to connect to container",
    },
  ])("shows $source errors and lets the operator retry", async ({ detail }) => {
    request.mockRejectedValueOnce(new Error(detail)).mockResolvedValueOnce(json({}));
    renderMenu();
    await openAction("Sleep container");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(detail);
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("prevents duplicate dispatch while a command is pending", async () => {
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    let finish!: (response: Record<string, unknown>) => void;
    request.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    await openAction("Wake container");
    const confirm = screen.getByRole("button", { name: "Confirm" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    expect(confirm).toBeDisabled();
    expect(request).toHaveBeenCalledTimes(1);
    finish(json({}));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
  });

  it("waits for controller acknowledgement after queue acceptance", async () => {
    let finish!: (response: unknown) => void;
    state.commandResult.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    await openAction("Sleep container");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(state.commandResult).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("button", { name: "Sending…" })).toBeDisabled();
    expect(state.pushToast).not.toHaveBeenCalled();
    expect(request).toHaveBeenCalledTimes(1);
    finish({ result: { status: "finished", successCount: 1, failureCount: 0, deviceResults: [] } });
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("does not resend a write when its result cannot be read", async () => {
    state.commandResult.mockRejectedValueOnce(new Error("node disconnected"));
    renderMenu();
    await openAction("Sleep container");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Command outcome unconfirmed");
    expect(state.pushToast).not.toHaveBeenCalled();
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("validates power bounds and preserves other target settings", async () => {
    request
      .mockResolvedValueOnce(
        json({
          watts: 1000,
          minimumWatts: 500,
          maximumWatts: 2000,
          performanceMode: "PERFORMANCE_MODE_EFFICIENCY",
          balanceBays: true,
          hashOnDisconnect: true,
        }),
      )
      .mockResolvedValueOnce(json({ watts: 1500 }));
    renderMenu();
    await openAction("Manage power");
    const input = screen.getByLabelText("Container power target");
    await waitFor(() => expect(input).toHaveValue(1000));
    const confirm = screen.getByRole("button", { name: "Confirm" });
    for (const value of ["", "-1", "2001"]) {
      fireEvent.change(input, { target: { value } });
      expect(confirm).toBeDisabled();
    }
    fireEvent.change(input, { target: { value: "1500" } });
    fireEvent.click(confirm);
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenNthCalledWith(
      2,
      {
        deviceIdentifier: "ct1/board",
        watts: 1500,
        performanceMode: "PERFORMANCE_MODE_EFFICIENCY",
        balanceBays: true,
        hashOnDisconnect: true,
      },
      expect.objectContaining({ timeoutMs: 10_000 }),
    );
  });

  it.each([0, undefined])("requires at least 1 W when the controller minimum is %s", async (minimumWatts) => {
    request
      .mockResolvedValueOnce(json({ watts: 0, minimumWatts, maximumWatts: 2000 }))
      .mockResolvedValueOnce(json({ watts: 1 }));
    renderMenu();
    await openAction("Manage power");
    const input = screen.getByLabelText("Container power target");
    await waitFor(() => expect(input).toBeEnabled());
    const confirm = screen.getByRole("button", { name: "Confirm" });
    for (const value of ["0", "0.5", "-1"]) {
      fireEvent.change(input, { target: { value } });
      expect(confirm).toBeDisabled();
      fireEvent.click(confirm);
      expect(request).toHaveBeenCalledTimes(1); // Only the target read; no write.
    }
    expect(screen.getByText("Minimum 1 W; maximum 2,000 W.")).toBeInTheDocument();
    fireEvent.change(input, { target: { value: "1" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ deviceIdentifier: "ct1/board", watts: 1 }),
      expect.objectContaining({ timeoutMs: 10_000 }),
    );
  });

  it.each([true, false])("offers only the applicable mining command when mining is %s", async (mining) => {
    statusRequest.mockResolvedValueOnce(json({ mining }));
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    const menu = await screen.findByTestId("container-ct1/board-actions-popover");
    await within(menu).findByRole("button", { name: mining ? "Sleep container" : "Wake container" });
    expect(
      within(menu)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual([
      "View container",
      mining ? "Sleep container" : "Wake container",
      "Manage power",
      "Edit pool",
      "Update firmware",
      "Update worker name",
      "Unpair",
    ]);
    expect(statusRequest).toHaveBeenCalledWith(
      { deviceIdentifier: "ct1/board" },
      expect.objectContaining({ timeoutMs: 10_000 }),
    );
  });

  it("hides both commands while status is loading and refreshes after a command", async () => {
    let finish!: (response: Record<string, unknown>) => void;
    statusRequest.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Wake container" })).not.toBeInTheDocument();
    finish(json({ mining: true }));
    fireEvent.click(await screen.findByRole("button", { name: "Sleep container" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(await screen.findByRole("button", { name: "Wake container" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(statusRequest).toHaveBeenCalledTimes(2);
  });

  it.each([
    ["MINING_OPERATING_STATE_STOPPED", true, "Wake container", "Sleep container"],
    ["MINING_OPERATING_STATE_NO_POOLS", false, "Sleep container", "Wake container"],
  ] as const)(
    "uses explicit %s status ahead of aggregate board mining",
    async (operatingState, mining, shown, hidden) => {
      statusRequest.mockResolvedValueOnce(json({ mining, operatingState }));
      renderMenu();
      fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
      expect(await screen.findByRole("button", { name: shown })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: hidden })).not.toBeInTheDocument();
    },
  );

  it("ignores an old status response after closing and reopening the menu", async () => {
    let finish!: (response: Record<string, unknown>) => void;
    statusRequest.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(statusRequest.mock.calls[0][1]?.signal?.aborted).toBe(true);
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "Wake container" })).toBeInTheDocument();
    finish(json({ mining: true }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument());
  });

  it("keeps permission-limited menus available for a later status refresh", async () => {
    state.permissions = ["miner:read", "miner:start_mining"];
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "No applicable commands" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    fireEvent.click(trigger);
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "Wake container" })).toBeInTheDocument();
  });

  it("keeps power available when the controller responds without a mining state", async () => {
    statusRequest.mockResolvedValueOnce(json({}));
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(await screen.findByRole("button", { name: "Container status unavailable" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Wake container" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Manage power" })).toBeEnabled();
  });

  it("opens native container details using the controller identifier", async () => {
    renderMenu({ site: { id: 1n, label: "West site" } });
    await openAction("View container");
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/ct1%2Fboard");
    expect(request).not.toHaveBeenCalled();
  });

  it("keeps unsupported settings and pools with unknown readiness disabled", async () => {
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    await screen.findByRole("button", { name: "Sleep container" });
    for (const name of ["Edit pool", "Update worker name"]) {
      const button = screen.getByRole("button", { name });
      expect(button).toBeDisabled();
      fireEvent.click(button);
    }
    expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument();
    expect(request).not.toHaveBeenCalled();
  });

  it("offers readers only the container view", () => {
    state.permissions = ["miner:read"];
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    const menu = screen.getByTestId("container-ct1/board-actions-popover");
    expect(
      within(menu)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual(["View container"]);
    expect(statusRequest).not.toHaveBeenCalled();
  });

  it.each([
    ["miner:reboot", "Update firmware"],
    ["miner:firmware_update", "Update firmware"],
    ["pool:read", "Edit pool"],
    ["miner:update_pools", "Edit pool"],
    ["miner:update_worker_names", "Update worker name"],
  ])("hides %s-dependent settings without that permission", async (permission, label) => {
    state.permissions = state.permissions.filter((key) => key !== permission);
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    await screen.findByRole("button", { name: "Sleep container" });
    expect(screen.queryByRole("button", { name: label })).not.toBeInTheDocument();
  });

  it("hides the menu without permission to read the container", () => {
    state.permissions = state.permissions.filter((key) => key !== "miner:read");
    renderMenu();
    expect(screen.queryByRole("button", { name: "Actions for Container 1" })).not.toBeInTheDocument();
  });

  it.each([
    [404, "miner not found or cannot be proxied"],
    [502, "Failed to connect to container"],
    [403, "Permission denied"],
  ])("keeps native navigation available when the command API returns %s", async (_code, error) => {
    statusRequest.mockRejectedValueOnce(new Error(error));
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    expect(trigger).toBeEnabled();
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "Container status unavailable" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Wake container" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Manage power" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Manage power" }));
    expect(request).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "View container" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/ct1%2Fboard");
  });

  it("checks command availability for operators with only power permissions", async () => {
    state.permissions = ["miner:read", "miner:set_power_target"];
    statusRequest.mockRejectedValueOnce(new Error("container not found"));
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    fireEvent.click(trigger);
    expect(screen.getByRole("button", { name: "Manage power" })).toBeDisabled();
    await waitFor(() => expect(statusRequest).toHaveBeenCalledTimes(1));
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    await waitFor(() => expect(screen.getByRole("button", { name: "Manage power" })).toBeEnabled());
    expect(statusRequest).toHaveBeenCalledTimes(2);
    expect(request).not.toHaveBeenCalled();
  });
});

describe("container unpair", () => {
  it("confirms and unpairs only the controller even when its controller is unavailable", async () => {
    statusRequest.mockRejectedValueOnce(new Error("Controller unavailable"));
    renderMenu();
    await openAction("Unpair");
    expect(state.deleteMiners).not.toHaveBeenCalled();
    const dialog = screen.getByTestId("container-unpair-dialog");
    expect(dialog).toHaveTextContent("Container 1");
    fireEvent.click(within(dialog).getByRole("button", { name: "Unpair" }));
    await waitFor(() => expect(screen.queryByTestId("container-unpair-dialog")).not.toBeInTheDocument());
    expect(state.pushToast).toHaveBeenCalledWith({
      message: "Container 1: container unpaired.",
      status: STATUSES.success,
    });
    expect(state.deleteMiners).toHaveBeenCalledExactlyOnceWith({
      deviceSelector: {
        selectionType: { case: "includeDevices", value: { deviceIdentifiers: ["ct1/board"] } },
      },
    });
    expect(request).not.toHaveBeenCalled();
    expect(screen.getByTestId("location")).toHaveTextContent("/");
  });

  it("cancels without unpairing", async () => {
    renderMenu();
    await openAction("Unpair");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByTestId("container-unpair-dialog")).not.toBeInTheDocument();
    expect(state.deleteMiners).not.toHaveBeenCalled();
  });

  it("hides Unpair without delete permission", async () => {
    state.permissions = ["miner:read"];
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(screen.queryByRole("button", { name: "Unpair" })).not.toBeInTheDocument();
  });

  it("does not require controller command permissions", async () => {
    state.permissions = ["miner:read", "miner:delete"];
    renderMenu();
    await openAction("Unpair");
    expect(screen.getByTestId("container-unpair-dialog")).toBeInTheDocument();
    expect(statusRequest).not.toHaveBeenCalled();
  });

  it("prevents duplicate submissions and dismissal while unpairing", async () => {
    let finish!: (result: { deletedCount: number }) => void;
    state.deleteMiners.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    await openAction("Unpair");
    const confirm = screen.getByRole("button", { name: "Unpair" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    expect(confirm).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(state.deleteMiners).toHaveBeenCalledTimes(1);
    finish({ deletedCount: 1 });
    await waitFor(() => expect(screen.queryByTestId("container-unpair-dialog")).not.toBeInTheDocument());
  });

  it("shows errors and allows retry", async () => {
    state.deleteMiners.mockRejectedValueOnce(new Error("Unable to unpair container"));
    const onUnpaired = vi.fn();
    renderMenu({}, onUnpaired);
    await openAction("Unpair");
    fireEvent.click(screen.getByRole("button", { name: "Unpair" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Unable to unpair container");
    expect(onUnpaired).not.toHaveBeenCalled();
    expect(state.pushToast).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Unpair" }));
    await waitFor(() => expect(screen.queryByTestId("container-unpair-dialog")).not.toBeInTheDocument());
    expect(state.deleteMiners).toHaveBeenCalledTimes(2);
    expect(onUnpaired).toHaveBeenCalledExactlyOnceWith("ct1/board");
  });

  it("rechecks permission before confirmation", async () => {
    const view = renderMenu();
    await openAction("Unpair");
    state.permissions = ["miner:read"];
    view.rerender(
      <MemoryRouter>
        <ContainerActionsMenu container={container} />
      </MemoryRouter>,
    );
    expect(screen.getByRole("button", { name: "Unpair" })).toBeDisabled();
    expect(state.deleteMiners).not.toHaveBeenCalled();
  });
});

it.each([true, false])("confirms default-group-only scope after activation (mining=%s)", async (mining) => {
  state.getState.mockResolvedValue({ enabled: true });
  statusRequest.mockImplementation(async () => json({ mining }));
  const verb = mining ? "Sleep" : "Wake";
  renderMenu();
  fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
  await screen.findByRole("button", { name: `${verb} default mining group` });
  expect(screen.queryByRole("button", { name: `${verb} container` })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: `${verb} default mining group` }));
  expect(screen.getByTestId("container-action-dialog")).toHaveTextContent(
    "Applies only to the default mining group. Other mining groups are unaffected.",
  );
  expect(screen.getByTestId("container-action-dialog")).not.toHaveTextContent("whole container");
});

it.each([true, false])("keeps legacy controls available when group observation fails (mining=%s)", async (mining) => {
  state.getState.mockRejectedValue(new Error("Fleet Node requires protocol v6"));
  statusRequest.mockImplementation(async () => json({ mining }));
  renderMenu();
  fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
  expect(await screen.findByRole("button", { name: mining ? "Sleep" : "Wake" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Manage power" })).toBeEnabled();
  expect(
    screen.queryByRole("button", { name: /default mining group|Sleep container|Wake container/ }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: mining ? "Sleep" : "Wake" }));
  expect(screen.getByTestId("container-action-dialog")).toHaveTextContent(
    "If mining groups are enabled, this affects only the default mining group.",
  );
  expect(screen.getByTestId("container-action-dialog")).not.toHaveTextContent("whole container");
});

it("does not wait for group observation before enabling legacy controls", async () => {
  state.getState.mockReturnValue(new Promise(() => {}));
  renderMenu();
  fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
  expect(await screen.findByRole("button", { name: "Sleep" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Manage power" })).toBeEnabled();
});

it.each([
  { supported: false, enabled: false, visible: false },
  { supported: true, enabled: false, visible: true },
  { supported: true, enabled: true, visible: false },
])("gates activation on controller support and state: %j", async ({ supported, enabled, visible }) => {
  state.permissions = ["miner:read", "miner:activate_mining_groups", "miner:start_mining", "miner:stop_mining"];
  state.getState.mockResolvedValue({ supported, enabled });
  renderMenu();
  fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
  await waitFor(() => expect(state.getState).toHaveBeenCalled());
  await waitFor(() => expect(!!screen.queryByRole("button", { name: "Enable mining groups" })).toBe(visible));
});

it.each(["miner:start_mining", "miner:stop_mining"])("hides activation without %s", async (missing) => {
  state.permissions = ["miner:read", "miner:activate_mining_groups", "miner:start_mining", "miner:stop_mining"].filter(
    (permission) => permission !== missing,
  );
  state.getState.mockResolvedValue({ supported: true, enabled: false });
  renderMenu();
  fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
  await waitFor(() => expect(state.getState).toHaveBeenCalled());
  expect(screen.queryByRole("button", { name: "Enable mining groups" })).not.toBeInTheDocument();
});

describe("whole-container pool configuration", () => {
  const ready = () => ({ supported: true, enabled: false, observedAt: timestampFromDate(new Date()), status: "idle" });

  it("opens the shared picker for the whole container without activation or start/stop permissions", async () => {
    state.permissions = ["miner:read", "pool:read", "miner:update_pools"];
    state.getState.mockResolvedValue(ready());
    renderMenu();
    await openAction("Edit pool");
    const dialog = screen.getByRole("dialog", { name: "Pool configuration" });
    expect(dialog).toHaveTextContent("ct1/board");
    expect(dialog).toHaveTextContent("Applies to the whole container.");
    expect(request).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel pool edit" }));
    expect(screen.queryByRole("dialog", { name: "Pool configuration" })).not.toBeInTheDocument();
  });

  it.each([
    { supported: false },
    { suspended: true },
    { observedAt: undefined },
    { observedAt: timestampFromDate(new Date(Date.now() - 180_000)) },
    { status: "pending" },
    { status: "applying" },
    { status: "blocked" },
  ])("disables whole-container editing for unavailable mode %#", async (mode) => {
    state.getState.mockResolvedValue({ ...ready(), ...mode });
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    await screen.findByRole("button", { name: /Sleep/ });
    expect(screen.getByRole("button", { name: "Edit pool" })).toBeDisabled();
  });

  it("offers assignment removal for an already-enabled container", async () => {
    state.getState.mockResolvedValue({ ...ready(), enabled: true });
    renderMenu();
    await openAction("Container pool assignment");
    expect(screen.getByRole("dialog", { name: "Pool configuration" })).toHaveTextContent("Remove assignment only");
    expect(screen.queryByRole("button", { name: "Edit pool" })).not.toBeInTheDocument();
  });

  it("keeps editing disabled if group observation fails", async () => {
    state.getState.mockRejectedValue(new Error("Controller unavailable"));
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    await screen.findByRole("button", { name: "Sleep" });
    expect(screen.getByRole("button", { name: "Edit pool" })).toBeDisabled();
  });

  it("keeps editing disabled until group mode is known", async () => {
    state.getState.mockReturnValue(new Promise(() => {}));
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(screen.getByRole("button", { name: "Edit pool" })).toBeDisabled();
  });
});

describe("container firmware update", () => {
  beforeEach(() => {
    state.firmwareUpdate.mockReset();
  });

  it("queues firmware for exactly the selected container", async () => {
    state.firmwareUpdate.mockResolvedValue({ batchIdentifier: "batch-fw" });
    renderMenu();
    await openAction("Update firmware");
    fireEvent.click(screen.getByRole("button", { name: "Confirm hashboard update" }));
    await waitFor(() =>
      expect(state.firmwareUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          firmwareFileId: "ct1-firmware",
          deviceSelector: expect.objectContaining({
            selectionType: {
              case: "includeDevices",
              value: expect.objectContaining({ deviceIdentifiers: [container.deviceIdentifier] }),
            },
          }),
        }),
      ),
    );
    await waitFor(() =>
      expect(state.pushToast).toHaveBeenCalledWith(expect.objectContaining({ status: STATUSES.success })),
    );
  });

  it("shows the server's message without the protocol code", async () => {
    state.firmwareUpdate.mockImplementation(async () => {
      throw new ConnectError("firmware targets another model", Code.FailedPrecondition);
    });
    renderMenu();
    await openAction("Update firmware");
    fireEvent.click(screen.getByRole("button", { name: "Confirm hashboard update" }));
    await waitFor(() =>
      expect(state.pushToast).toHaveBeenCalledWith({
        message: "firmware targets another model",
        status: STATUSES.error,
      }),
    );
  });
});
