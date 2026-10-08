import { StrictMode } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import TankActionsMenu from "./TankActionsMenu";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
const mocks = vi.hoisted(() => ({ options: vi.fn(), submit: vi.fn(), results: vi.fn(), permissions: [] as string[] }));
vi.mock("@/protoFleet/api/clients", () => ({
  containersClient: {
    getTankMiningOptions: mocks.options,
    setTankMining: mocks.submit,
    getEquipmentCommandResult: mocks.results,
  },
}));
vi.mock("@/protoFleet/store", () => ({ usePermissions: () => mocks.permissions }));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));
const tanks = [
  create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 11n }, name: "Tank 1", facilityName: "North CT1" }),
  create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 13n }, name: "Tank 3", facilityName: "South CT1" }),
];
beforeEach(() => {
  vi.clearAllMocks();
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
  it("confirms exact selected tank identities and surfaces mixed-controller partial failure", async () => {
    const refresh = vi.fn();
    render(
      <StrictMode>
        <TankActionsMenu tanks={tanks} onRefresh={refresh} />
      </StrictMode>,
    );
    await openStop();
    expect(mocks.submit).not.toHaveBeenCalled();
    const dialog = screen.getByTestId("tank-mining-dialog");
    expect(dialog).toHaveTextContent("North CT1: Tank 1");
    expect(dialog).toHaveTextContent("South CT1: Tank 3");
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await screen.findByText(/North CT1: Tank 1: Stop mining acknowledged/);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "South CT1: Tank 3: Tank partially applied: board disconnected",
    );
    expect(mocks.submit).toHaveBeenCalledTimes(1);
    expect(mocks.submit).toHaveBeenCalledWith({ tankIds: [11n, 13n], mining: false }, expect.anything());
    expect(refresh).toHaveBeenCalledOnce();
  });
  it("cancels before dispatch", async () => {
    render(<TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />);
    await openStop();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(mocks.submit).not.toHaveBeenCalled();
  });
  it.each(["trigger", "action"])("dismisses the menu with Escape from the focused %s", async (target) => {
    render(<TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />);
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
  it.each(["Cancel", "Confirm"])("dismisses confirmation with Escape from the focused %s button", async (label) => {
    render(<TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />);
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
        <TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />
      </div>,
    );
    fireEvent.keyDown(screen.getByRole("button", { name: "Actions for 2 selected tanks" }), { key });
    expect(rowKeyDown).not.toHaveBeenCalled();
  });
  it("disables unavailable scope and excludes unsupported container operations", async () => {
    mocks.options.mockResolvedValue({ canStart: false, canStop: false, unavailableReason: "Topology stale" });
    render(<TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />);
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
    render(<TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Actions for 2 selected tanks" }));
    await waitFor(() => expect(screen.getByRole("button", { name: action })).toBeEnabled());
    expect(mocks.options).toHaveBeenCalledWith({ tankIds: [11n, 13n] }, expect.anything());
    expect(screen.queryByRole("button", { name: other })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: action }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await screen.findByText(new RegExp(`North CT1: Tank 1: ${action} acknowledged`));
    expect(mocks.submit).toHaveBeenCalledWith({ tankIds: [11n, 13n], mining }, expect.anything());
  });
  it("keeps writes disabled when resource authorization is denied", async () => {
    mocks.permissions = ["site:read", "fleet:read", "miner:read"];
    mocks.options.mockRejectedValue(new Error("Tank actions denied"));
    render(<TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />);
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
    render(<TankActionsMenu tanks={tanks} onRefresh={vi.fn()} />);
    await openStop();
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Connection lost");
    expect(screen.queryByRole("button", { name: "Confirm" })).not.toBeInTheDocument();
    expect(mocks.submit).toHaveBeenCalledOnce();
  });
});
