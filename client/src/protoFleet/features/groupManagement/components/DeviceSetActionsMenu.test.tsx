import { Fragment, type ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import DeviceSetActionsMenu from "./DeviceSetActionsMenu";

// Hoisted mocks
const { mockUseMinerActions, mockBulkActionsPopover } = vi.hoisted(() => ({
  mockUseMinerActions: vi.fn(() => ({
    currentAction: null,
    popoverActions: [],
    handleConfirmation: vi.fn(),
    handleCancel: vi.fn(),
    handleMiningPoolSuccess: vi.fn(),
    handleMiningPoolError: vi.fn(),
    showPoolSelectionPage: false,
    poolFilteredDeviceIds: undefined,
    fleetCredentials: undefined,
    showManagePowerModal: false,
    handleManagePowerConfirm: vi.fn(),
    handleManagePowerDismiss: vi.fn(),
    showCoolingModeModal: false,
    coolingModeCount: 0,
    currentCoolingMode: undefined,
    handleCoolingModeConfirm: vi.fn(),
    handleCoolingModeDismiss: vi.fn(),
    showAuthenticateFleetModal: false,
    authenticationPurpose: null,
    showUpdatePasswordModal: false,
    hasThirdPartyMiners: false,
    handleFleetAuthenticated: vi.fn(),
    handlePasswordConfirm: vi.fn(),
    handlePasswordDismiss: vi.fn(),
    handleAuthDismiss: vi.fn(),
    unsupportedMinersInfo: { visible: false, unsupportedGroups: [], totalUnsupportedCount: 0, noneSupported: false },
    handleUnsupportedMinersContinue: vi.fn(),
    handleUnsupportedMinersDismiss: vi.fn(),
    showManageSecurityModal: false,
    minerGroups: [],
    handleUpdateGroup: vi.fn(),
    handleSecurityModalClose: vi.fn(),
  })),
  mockBulkActionsPopover: vi.fn(
    ({
      actions,
      beforeEach: beforeEachAction,
    }: {
      actions: Array<{
        action: string;
        title: string;
        actionHandler: () => void;
        requiresConfirmation: boolean;
      }>;
      beforeEach: (requiresConfirmation: boolean) => void;
    }) => (
      <div data-testid="group-actions-popover">
        {actions.map((action) => (
          <button
            key={action.action}
            data-testid={`${action.action}-popover-button`}
            onClick={() => {
              beforeEachAction(action.requiresConfirmation);
              action.actionHandler();
            }}
          >
            {action.title}
          </button>
        ))}
      </div>
    ),
  ),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/BulkActions", () => ({
  BulkActionsPopover: mockBulkActionsPopover,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/BulkActions/BulkActionConfirmDialog", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/BulkActions/UnsupportedMinersModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/CoolingModeModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManagePowerModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManageSecurity", () => ({
  ManageSecurityModal: () => null,
  UpdateMinerPasswordModal: () => null,
}));

vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listGroupMembers: vi.fn() }),
}));

vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => <Fragment>{children}</Fragment>,
  usePopover: () => ({
    triggerRef: { current: null },
    setPopoverRenderMode: vi.fn(),
  }),
}));

vi.mock("@/shared/hooks/useClickOutside", () => ({
  useClickOutside: vi.fn(),
}));

describe("DeviceSetActionsMenu", () => {
  it("renders 'View group' action when onView is provided", () => {
    const onEdit = vi.fn();
    const onView = vi.fn();

    render(<DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} onView={onView} />);

    // Open the menu
    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.getByTestId("view-group-popover-button")).toBeInTheDocument();
    expect(screen.getByTestId("view-group-popover-button")).toHaveTextContent("View group");
  });

  it("calls onView when 'View group' is clicked", () => {
    const onEdit = vi.fn();
    const onView = vi.fn();

    render(<DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} onView={onView} />);

    // Open the menu
    fireEvent.click(screen.getByLabelText("Device set actions"));

    // Click "View group"
    fireEvent.click(screen.getByTestId("view-group-popover-button"));

    expect(onView).toHaveBeenCalledTimes(1);
  });

  it("does not render 'View group' action when onView is not provided", () => {
    const onEdit = vi.fn();

    render(<DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} />);

    // Open the menu
    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.queryByTestId("view-group-popover-button")).not.toBeInTheDocument();
  });

  it("uses custom viewLabel when provided", () => {
    const onEdit = vi.fn();
    const onView = vi.fn();

    render(
      <DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} onView={onView} viewLabel="View rack" />,
    );

    // Open the menu
    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.getByTestId("view-group-popover-button")).toHaveTextContent("View rack");
  });
});
