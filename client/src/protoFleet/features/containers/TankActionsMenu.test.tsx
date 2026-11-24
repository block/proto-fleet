import { StrictMode } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import MiningActionsMenu from "./MiningActionsMenu";
import { runMiningAction } from "./runMiningAction";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { useMountedCallback } from "@/shared/hooks/useMountedCallback";
const mocks = vi.hoisted(() => ({
  options: vi.fn(),
  submit: vi.fn(),
  results: vi.fn(),
  pushToast: vi.fn(),
  updateToast: vi.fn(),
  removeToast: vi.fn(),
  permissions: [] as string[],
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: mocks.pushToast,
  updateToast: mocks.updateToast,
  removeToast: mocks.removeToast,
  STATUSES: { loading: "loading", success: "success", error: "error" },
}));
vi.mock("@/protoFleet/api/clients", () => ({
  containersClient: {
    getTankMiningOptions: mocks.options,
    setTankMining: mocks.submit,
    getEquipmentCommandResult: mocks.results,
  },
}));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  usePermissions: () => mocks.permissions,
}));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));
const tanks = [
  create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 11n }, name: "Tank 1", facilityName: "North CT1" }),
  create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 13n }, name: "Tank 3", facilityName: "South CT1" }),
];
beforeEach(() => {
  vi.clearAllMocks();
  let toastId = 0;
  mocks.pushToast.mockReset().mockImplementation(() => ++toastId);
  mocks.permissions = ["site:read", "fleet:read", "miner:read", "miner:start_mining", "miner:stop_mining"];
  mocks.options.mockResolvedValue({ canStart: true, canStop: true, unavailableReason: "" });
  mocks.submit.mockResolvedValue({
    commands: [
      { deviceIdentifier: "north", containerName: "North CT1", tankIds: [11n], batchIdentifier: "a" },
      { deviceIdentifier: "south", containerName: "South CT1", tankIds: [13n], batchIdentifier: "b" },
    ],
  });
  mocks.results.mockImplementation(async ({ deviceIdentifier }) => ({
    result: {
      status: "finished",
      successCount: deviceIdentifier === "north" ? 1 : 0,
      failureCount: deviceIdentifier === "south" ? 1 : 0,
      deviceResults:
        deviceIdentifier === "south" ? [{ errorMessage: "Tank partially applied: board disconnected" }] : [],
    },
  }));
});
async function openStop() {
  fireEvent.click(screen.getByRole("button", { name: "Actions for 2 selected tanks" }));
  const stop = await screen.findByRole("button", { name: "Stop mining" });
  await waitFor(() => expect(stop).toBeEnabled());
  fireEvent.click(stop);
}
describe("tank actions", () => {
  it("confirms the tank count without listing tanks and preserves known success with an unknown controller outcome", async () => {
    const refresh = vi.fn();
    render(
      <StrictMode>
        <MiningActionsMenu kind="tank" items={tanks} onRefresh={refresh} />
      </StrictMode>,
    );
    await openStop();
    expect(mocks.submit).not.toHaveBeenCalled();
    const dialog = screen.getByTestId("tank-mining-dialog");
    expect(dialog).toHaveTextContent("Stop mining on 2 tanks?");
    expect(dialog).toHaveTextContent("All modules in these tanks will stop hashing.");
    expect(within(dialog).queryByRole("list")).not.toBeInTheDocument();
    expect(dialog).not.toHaveTextContent("Tank 1");
    expect(dialog).not.toHaveTextContent("Tank 3");
    fireEvent.click(within(dialog).getByRole("button", { name: "Stop mining" }));
    expect(screen.queryByTestId("tank-mining-dialog")).not.toBeInTheDocument();
    await waitFor(() =>
      expect(mocks.updateToast).toHaveBeenCalledWith(
        1,
        expect.objectContaining({
          status: "error",
          message:
            "Stopped mining on 1 out of 2 tanks. Unable to confirm the remaining tanks. Check tank status before trying again.",
        }),
      ),
    );
    expect(mocks.pushToast).toHaveBeenCalledOnce();
    expect(mocks.updateToast.mock.calls.some(([, toast]) => toast.status === "success" || toast.actions)).toBe(false);
    expect(JSON.stringify(mocks.updateToast.mock.calls)).not.toMatch(/North CT1|South CT1|Tank 1|Tank 3/);
    expect(mocks.submit).toHaveBeenCalledTimes(1);
    expect(mocks.submit).toHaveBeenCalledWith({ tankIds: [11n, 13n], mining: false }, expect.anything());
    expect(refresh).toHaveBeenCalledOnce();
  });
  it("cancels before dispatch", async () => {
    const complete = vi.fn();
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} onActionComplete={complete} />);
    await openStop();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(mocks.submit).not.toHaveBeenCalled();
    expect(complete).toHaveBeenCalledOnce();
  });
  it.each(["trigger", "action"])("dismisses the menu with Escape from the focused %s", async (target) => {
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />);
    const trigger = screen.getByRole("button", { name: "Actions for 2 selected tanks" });
    fireEvent.click(trigger);
    await waitFor(() => expect(screen.getByRole("button", { name: "Stop mining" })).toBeEnabled());
    const control = target === "trigger" ? trigger : screen.getByRole("button", { name: "Stop mining" });
    control.focus();
    fireEvent.keyDown(control, { key: "Escape" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("tank-11,13-actions-popover")).not.toBeInTheDocument();
    expect(mocks.submit).not.toHaveBeenCalled();
  });
  it.each(["Cancel", "Stop mining"])("dismisses confirmation with Escape from the focused %s button", async (label) => {
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />);
    await openStop();
    const control = within(screen.getByTestId("tank-mining-dialog")).getByRole("button", { name: label });
    control.focus();
    fireEvent.keyDown(control, { key: "Escape" });
    expect(screen.queryByTestId("tank-mining-dialog")).not.toBeInTheDocument();
    expect(mocks.submit).not.toHaveBeenCalled();
  });
  it.each(["Enter", " "])("keeps %j from activating the containing row", (key) => {
    const rowKeyDown = vi.fn();
    render(
      <div onKeyDown={rowKeyDown}>
        <MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />
      </div>,
    );
    fireEvent.keyDown(screen.getByRole("button", { name: "Actions for 2 selected tanks" }), { key });
    expect(rowKeyDown).not.toHaveBeenCalled();
  });
  it("disables unavailable scope and excludes unsupported container operations", async () => {
    mocks.options.mockResolvedValue({ canStart: false, canStop: false, unavailableReason: "Topology stale" });
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Actions for 2 selected tanks" }));
    expect(await screen.findByRole("button", { name: "Topology stale" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Stop mining" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Reboot" })).not.toBeInTheDocument();
  });
  it.each([
    { action: "Start mining", other: "Stop mining", mining: true, permissions: [] },
    {
      action: "Stop mining",
      other: "Start mining",
      mining: false,
      permissions: ["site:read", "fleet:read", "miner:read"],
    },
  ])("allows $action granted at the selected tanks' sites", async ({ action, other, mining, permissions }) => {
    mocks.permissions = permissions;
    mocks.options.mockResolvedValue({ canStart: mining, canStop: !mining, unavailableReason: "" });
    mocks.results.mockResolvedValue({ result: { status: "finished", successCount: 1, failureCount: 0 } });
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Actions for 2 selected tanks" }));
    await waitFor(() => expect(screen.getByRole("button", { name: action })).toBeEnabled());
    expect(mocks.options).toHaveBeenCalledWith({ tankIds: [11n, 13n] }, expect.anything());
    expect(screen.queryByRole("button", { name: other })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: action }));
    fireEvent.click(within(screen.getByTestId("tank-mining-dialog")).getByRole("button", { name: action }));
    await waitFor(() =>
      expect(mocks.updateToast).toHaveBeenCalledWith(
        1,
        expect.objectContaining({
          status: "success",
          message: `${mining ? "Started" : "Stopped"} mining on 2 out of 2 tanks`,
        }),
      ),
    );
    expect(mocks.submit).toHaveBeenCalledWith({ tankIds: [11n, 13n], mining }, expect.anything());
  });
  it("keeps writes disabled when resource authorization is denied", async () => {
    mocks.permissions = ["site:read", "fleet:read", "miner:read"];
    mocks.options.mockRejectedValue(new Error("Tank actions denied"));
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Actions for 2 selected tanks" }));
    expect(await screen.findByRole("button", { name: "Tank actions denied" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Start mining" })).toBeDisabled();
    const stop = screen.getByRole("button", { name: "Stop mining" });
    expect(stop).toBeDisabled();
    fireEvent.click(stop);
    expect(screen.queryByTestId("tank-mining-dialog")).not.toBeInTheDocument();
    expect(mocks.submit).not.toHaveBeenCalled();
  });
  it("does not offer a retry when dispatch outcome is uncertain", async () => {
    mocks.submit.mockRejectedValue(new Error("Connection lost"));
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />);
    await openStop();
    fireEvent.click(within(screen.getByTestId("tank-mining-dialog")).getByRole("button", { name: "Stop mining" }));
    await waitFor(() =>
      expect(mocks.updateToast).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ status: "error", message: expect.stringContaining("Connection lost") }),
      ),
    );
    expect(mocks.updateToast.mock.calls.every(([, toast]) => !toast.actions)).toBe(true);
    expect(screen.queryByRole("button", { name: "Stop mining" })).not.toBeInTheDocument();
    expect(mocks.submit).toHaveBeenCalledOnce();
  });
  it("shows completed counts only after both controller batches finish", async () => {
    mocks.results.mockResolvedValue({ result: { status: "finished", successCount: 1, failureCount: 0 } });
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} />);
    await openStop();
    const dialog = screen.getByTestId("tank-mining-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Stop mining" }));
    await waitFor(() =>
      expect(mocks.updateToast).toHaveBeenCalledWith(
        1,
        expect.objectContaining({
          status: "success",
          message: "Stopped mining on 2 out of 2 tanks",
        }),
      ),
    );
    expect(mocks.updateToast.mock.calls.filter(([, toast]) => toast.status === "success")).toHaveLength(1);
    expect(mocks.pushToast).toHaveBeenCalledOnce();
    expect(mocks.results).toHaveBeenCalledTimes(2);
    expect(JSON.stringify(mocks.updateToast.mock.calls)).not.toMatch(/CT1|Tank 1|Tank 3/);
  });
  it("closes immediately and refreshes the data owner after its row unmounts", async () => {
    let completeDispatch!: () => void;
    mocks.submit.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          completeDispatch = () =>
            resolve({ commands: [{ deviceIdentifier: "north", tankIds: [11n, 13n], batchIdentifier: "a" }] });
        }),
    );
    const refresh = vi.fn();
    const complete = vi.fn();
    const { unmount } = render(
      <MiningActionsMenu kind="tank" items={tanks} onRefresh={refresh} onActionComplete={complete} />,
    );
    await openStop();
    const confirm = within(screen.getByTestId("tank-mining-dialog")).getByRole("button", { name: "Stop mining" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    expect(screen.queryByTestId("tank-mining-dialog")).not.toBeInTheDocument();
    expect(complete).not.toHaveBeenCalled();
    expect(mocks.submit).toHaveBeenCalledOnce();
    expect(mocks.results).not.toHaveBeenCalled();
    expect(mocks.pushToast).toHaveBeenCalledWith(
      expect.objectContaining({ status: "loading", message: "Stopping mining on 2 tanks" }),
    );
    unmount();
    completeDispatch();
    await waitFor(() =>
      expect(mocks.updateToast).toHaveBeenCalledWith(1, expect.objectContaining({ status: "success" })),
    );
    expect(refresh).toHaveBeenCalledOnce();
    expect(complete).not.toHaveBeenCalled();
  });
  it("reports completion without refreshing the old page after navigation during result polling", async () => {
    let finish!: () => void;
    mocks.submit.mockResolvedValueOnce({
      commands: [{ deviceIdentifier: "north", tankIds: [11n, 13n], batchIdentifier: "a" }],
    });
    mocks.results.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({ result: { status: "finished", successCount: 1, failureCount: 0 } });
        }),
    );
    const refresh = vi.fn();
    const Page = () => {
      const refreshPage = useMountedCallback(refresh);
      return <MiningActionsMenu kind="tank" items={tanks} onRefresh={refreshPage} />;
    };
    const { unmount } = render(<Page />);
    await openStop();
    fireEvent.click(within(screen.getByTestId("tank-mining-dialog")).getByRole("button", { name: "Stop mining" }));
    await waitFor(() => expect(mocks.results).toHaveBeenCalledOnce());
    unmount();
    finish();
    await waitFor(() =>
      expect(mocks.updateToast).toHaveBeenCalledWith(1, expect.objectContaining({ status: "success" })),
    );
    expect(refresh).not.toHaveBeenCalled();
  });
  it("restores the action bar after admission, independently of toast dismissal", async () => {
    let admit!: () => void;
    let finish!: () => void;
    mocks.submit.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          admit = () =>
            resolve({ commands: [{ deviceIdentifier: "north", tankIds: [11n, 13n], batchIdentifier: "a" }] });
        }),
    );
    mocks.results.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({ result: { status: "finished", successCount: 1, failureCount: 0 } });
        }),
    );
    const complete = vi.fn();
    render(<MiningActionsMenu kind="tank" items={tanks} onRefresh={vi.fn()} onActionComplete={complete} />);
    await openStop();
    fireEvent.click(within(screen.getByTestId("tank-mining-dialog")).getByRole("button", { name: "Stop mining" }));
    expect(screen.queryByTestId("tank-mining-dialog")).not.toBeInTheDocument();
    const toast = mocks.pushToast.mock.calls[0][0];
    toast.onClose?.();
    expect(complete).not.toHaveBeenCalled();
    admit();
    await waitFor(() => expect(complete).toHaveBeenCalledOnce());
    expect(mocks.updateToast).not.toHaveBeenCalled();
    finish();
    await waitFor(() =>
      expect(mocks.updateToast).toHaveBeenCalledWith(1, expect.objectContaining({ status: "success" })),
    );
    toast.onClose?.();
    expect(complete).toHaveBeenCalledOnce();
  });
  it.each([false, true])("tracks a pending result through %s timeout without repeating the write", async (timeout) => {
    vi.useFakeTimers();
    try {
      mocks.submit.mockResolvedValue({
        commands: [{ deviceIdentifier: "north", tankIds: [11n, 13n], batchIdentifier: "a" }],
      });
      mocks.results.mockResolvedValue({ result: { status: "processing", successCount: 1, failureCount: 0 } });
      const refresh = vi.fn();
      const execution = runMiningAction([11n, 13n], "tank", false, refresh);
      await vi.advanceTimersByTimeAsync(0);
      expect(mocks.updateToast).not.toHaveBeenCalled();
      if (!timeout) {
        mocks.results.mockResolvedValue({ result: { status: "finished", successCount: 1, failureCount: 0 } });
      }
      await vi.advanceTimersByTimeAsync(timeout ? 120_000 : 1000);
      await execution;
      expect(mocks.updateToast).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ status: timeout ? "error" : "success" }),
      );
      expect(mocks.submit).toHaveBeenCalledOnce();
      expect(refresh).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("tank bulk command results", () => {
  it.each([false, true])("tolerates timed-out result reads through completion-window expiry: %s", async (expire) => {
    vi.useFakeTimers();
    try {
      mocks.results.mockRejectedValue(new ConnectError("Ownership lock wait", Code.DeadlineExceeded));
      const refresh = vi.fn();
      const execution = runMiningAction([11n, 13n], "tank", false, refresh);
      await vi.advanceTimersByTimeAsync(0);
      expect(mocks.updateToast).not.toHaveBeenCalled();
      if (!expire) {
        mocks.results.mockResolvedValue({ result: { status: "finished", successCount: 1, failureCount: 0 } });
      }
      await vi.advanceTimersByTimeAsync(expire ? 120_000 : 1000);
      await execution;
      expect(mocks.updateToast).toHaveBeenLastCalledWith(
        1,
        expect.objectContaining({ status: expire ? "error" : "success" }),
      );
      expect(mocks.results.mock.calls.length).toBeGreaterThan(2);
      expect(mocks.submit).toHaveBeenCalledOnce();
      expect(refresh).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps completed controller counts loading until the remaining controller finishes", async () => {
    vi.useFakeTimers();
    try {
      mocks.results.mockImplementation(async ({ deviceIdentifier }) => ({
        result: { status: deviceIdentifier === "north" ? "finished" : "processing", successCount: 1, failureCount: 0 },
      }));
      const execution = runMiningAction([11n, 13n], "tank", true, vi.fn());
      await vi.advanceTimersByTimeAsync(0);
      expect(mocks.updateToast).toHaveBeenCalledWith(1, {
        message: "Started mining on 1 out of 2 tanks",
        status: "loading",
      });
      expect(mocks.updateToast.mock.calls.some(([, toast]) => toast.status === "success")).toBe(false);
      mocks.results.mockResolvedValue({ result: { status: "finished", successCount: 1, failureCount: 0 } });
      await vi.advanceTimersByTimeAsync(1000);
      await execution;
      expect(mocks.updateToast).toHaveBeenLastCalledWith(1, {
        message: "Started mining on 2 out of 2 tanks",
        status: "success",
      });
      expect(mocks.submit).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it.each(["polling interrupted", "pruned results", "missing counts"])(
    "does not show success or offer retry with %s, even after another controller succeeds",
    async (outcome) => {
      mocks.results.mockImplementation(async ({ deviceIdentifier }) => {
        if (deviceIdentifier === "north") return { result: { status: "finished", successCount: 1, failureCount: 0 } };
        if (outcome === "polling interrupted") throw new Error("Connection lost");
        return {
          result: { status: "finished", successCount: 0, failureCount: 0, detailsPruned: outcome === "pruned results" },
        };
      });
      await runMiningAction([11n, 13n], "tank", false, vi.fn());
      expect(mocks.updateToast).toHaveBeenLastCalledWith(1, {
        message:
          "Stopped mining on 1 out of 2 tanks. Unable to confirm the remaining tanks. Check tank status before trying again.",
        status: "error",
      });
      expect(mocks.updateToast.mock.calls.some(([, toast]) => toast.status === "success" || toast.actions)).toBe(false);
      expect(mocks.submit).toHaveBeenCalledOnce();
    },
  );

  it.each(["partially applied", "incomplete board results", "write response lost"])(
    "does not count either tank as failed or offer retry when their shared controller reports %s",
    async (errorMessage) => {
      mocks.submit.mockResolvedValue({
        commands: [{ deviceIdentifier: "north", tankIds: [11n, 13n], batchIdentifier: "a" }],
      });
      mocks.results.mockResolvedValue({
        result: { status: "finished", successCount: 0, failureCount: 1, deviceResults: [{ errorMessage }] },
      });
      const refresh = vi.fn();
      await runMiningAction([11n, 13n], "tank", false, refresh);
      expect(mocks.updateToast).toHaveBeenLastCalledWith(1, {
        message: "Unable to confirm bulk action completion. Check tank status before trying again.",
        status: "error",
      });
      expect(mocks.updateToast.mock.calls.some(([, toast]) => toast.status === "success" || toast.actions)).toBe(false);
      expect(JSON.stringify(mocks.updateToast.mock.calls)).not.toMatch(/Couldn't stop|mining on 2/);
      expect(mocks.pushToast).toHaveBeenCalledOnce();
      expect(mocks.submit).toHaveBeenCalledOnce();
      expect(refresh).toHaveBeenCalledOnce();
    },
  );

  it("reports unconfirmed completion without retry when all controllers report a terminal failure", async () => {
    mocks.results.mockResolvedValue({ result: { status: "finished", successCount: 0, failureCount: 1 } });
    await runMiningAction([11n, 13n], "tank", true, vi.fn());
    expect(mocks.updateToast).toHaveBeenLastCalledWith(1, {
      message: "Unable to confirm bulk action completion. Check tank status before trying again.",
      status: "error",
    });
    expect(mocks.updateToast.mock.calls.some(([, toast]) => toast.status === "success" || toast.actions)).toBe(false);
    expect(mocks.removeToast).not.toHaveBeenCalled();
    expect(mocks.submit).toHaveBeenCalledOnce();
  });

  it("treats terminal failures and interrupted polling as unknown outcomes without offering retry", async () => {
    mocks.results.mockImplementation(async ({ deviceIdentifier }) => {
      if (deviceIdentifier === "north") throw new Error("Connection lost");
      return { result: { status: "finished", successCount: 0, failureCount: 1 } };
    });
    await runMiningAction([11n, 13n], "tank", false, vi.fn());
    expect(mocks.pushToast).toHaveBeenCalledOnce();
    expect(mocks.updateToast).toHaveBeenLastCalledWith(
      1,
      expect.objectContaining({ status: "error", message: expect.stringContaining("Unable to confirm") }),
    );
    expect(mocks.updateToast.mock.calls.some(([, toast]) => toast.actions)).toBe(false);
  });
});
