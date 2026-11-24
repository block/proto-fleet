import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import MinerActionsMenu from "./MinerActionsMenu";

const { permissions, handleConfirmation } = vi.hoisted(() => ({
  permissions: { current: ["miner:read"] },
  handleConfirmation: vi.fn(),
}));

vi.mock("@/protoFleet/store", () => ({ usePermissions: () => permissions.current }));
vi.mock("@/protoFleet/features/fleetManagement/hooks/useBatchOperations", () => ({
  useBatchActions: () => ({}),
}));
vi.mock("./useMinerActions", async () => {
  const { ACTION_PERMISSIONS } = await import("./actionPermissions");
  return {
    useMinerActions: () => ({
      currentAction: "reboot",
      popoverActions: Object.keys(ACTION_PERMISSIONS).map((action) => ({
        action,
        title: action,
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: action === "reboot",
        confirmation:
          action === "reboot"
            ? {
                title: "Reboot miners?",
                confirmAction: { title: "Confirm reboot", variant: "primary" },
                testId: "confirm-reboot",
              }
            : undefined,
      })),
      handleConfirmation,
      handleCancel: vi.fn(),
    }),
  };
});
vi.mock("./MinerActionModalStack", () => ({ default: () => null }));
vi.mock("./MinerReparentPicker", () => ({ default: () => null }));
vi.mock("./BulkRenameModal", () => ({ default: () => null }));
vi.mock("./BulkWorkerNameModal", () => ({ default: () => null }));
vi.mock("../ActionBar/SettingsWidget/PoolSelectionPage", () => ({ default: () => null }));
vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({ default: () => null }));

const menu = <MinerActionsMenu selectedMiners={["miner-1"]} selectionMode="subset" />;

beforeEach(() => {
  permissions.current = ["miner:read"];
  handleConfirmation.mockClear();
});

test("hides the bulk trigger when real permission filtering removes every miner action", () => {
  render(menu);

  expect(screen.queryByTestId("actions-menu-button")).not.toBeInTheDocument();
  expect(screen.queryByTestId("actions-menu-popover")).not.toBeInTheDocument();
  expect(screen.queryByTestId("actions-menu-quick-action-reboot")).not.toBeInTheDocument();
});

test("closes an open menu when permissions disappear and keeps it closed if they return", () => {
  permissions.current = ["miner:read", "miner:reboot"];
  const { rerender } = render(menu);
  fireEvent.click(screen.getByTestId("actions-menu-button"));
  expect(screen.getByTestId("actions-menu-popover")).toBeInTheDocument();

  permissions.current = ["miner:read"];
  rerender(<MinerActionsMenu selectedMiners={["miner-1"]} selectionMode="subset" />);
  expect(screen.queryByTestId("actions-menu-button")).not.toBeInTheDocument();
  expect(screen.queryByTestId("actions-menu-popover")).not.toBeInTheDocument();

  permissions.current = ["miner:read", "miner:reboot"];
  rerender(<MinerActionsMenu selectedMiners={["miner-1"]} selectionMode="subset" />);
  expect(screen.getByTestId("actions-menu-button")).toBeInTheDocument();
  expect(screen.queryByTestId("actions-menu-popover")).not.toBeInTheDocument();
  fireEvent.click(screen.getByTestId("actions-menu-button"));
  expect(screen.getByTestId("reboot-popover-button")).toBeInTheDocument();
});

test("keeps permitted actions and their confirmation flow available", () => {
  permissions.current = ["miner:read", "miner:reboot"];
  render(menu);
  fireEvent.click(screen.getByTestId("actions-menu-button"));
  expect(screen.queryByTestId("blink-leds-popover-button")).not.toBeInTheDocument();
  fireEvent.click(screen.getByTestId("reboot-popover-button"));
  expect(screen.queryByTestId("actions-menu-popover")).not.toBeInTheDocument();
  expect(screen.getByText("Reboot miners?")).toBeInTheDocument();
  fireEvent.click(screen.getByTestId("confirm-reboot"));
  expect(handleConfirmation).toHaveBeenCalledOnce();
});
