import { Fragment, type ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import SingleMinerActionsMenu from "./SingleMinerActionsMenu";

const mockNavigate = vi.fn();

const { mockUseMinerActions } = vi.hoisted(() => ({
  mockUseMinerActions: vi.fn(() => ({
    currentAction: null,
    popoverActions: [],
    handleConfirmation: vi.fn(),
    handleCancel: vi.fn(),
    handleMiningPoolSuccess: vi.fn(),
    handleMiningPoolError: vi.fn(),
    showPoolSelectionPage: false,
    fleetCredentials: undefined,
    showManagePowerModal: false,
    handleManagePowerConfirm: vi.fn(),
    handleManagePowerDismiss: vi.fn(),
    showFirmwareUpdateModal: false,
    handleFirmwareUpdateConfirm: vi.fn(),
    handleFirmwareUpdateDismiss: vi.fn(),
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
    unsupportedMinersInfo: {
      visible: false,
      unsupportedGroups: [],
      totalUnsupportedCount: 0,
      noneSupported: false,
    },
    handleUnsupportedMinersContinue: vi.fn(),
    handleUnsupportedMinersDismiss: vi.fn(),
    showManageSecurityModal: false,
    minerGroups: [],
    handleUpdateGroup: vi.fn(),
    handleSecurityModalClose: vi.fn(),
    showRenameDialog: false,
    handleRenameOpen: vi.fn(),
    handleRenameConfirm: vi.fn(),
    handleRenameDismiss: vi.fn(),
    showAddToGroupModal: false,
    handleAddToGroupDismiss: vi.fn(),
  })),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => mockNavigate,
}));

vi.mock("./useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

vi.mock("@/protoFleet/store/hooks/useFleet", () => ({
  useMinerDeviceStatus: vi.fn(() => undefined),
}));

vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => <Fragment>{children}</Fragment>,
  usePopover: () => ({
    triggerRef: { current: null },
    setPopoverRenderMode: vi.fn(),
  }),
  popoverSizes: { small: "small" },
  default: ({ children, testId }: { children: ReactNode; testId?: string }) => (
    <div data-testid={testId}>{children}</div>
  ),
}));

vi.mock("@/shared/hooks/useClickOutside", () => ({
  useClickOutside: vi.fn(),
}));

vi.mock("../ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./RenameMinerDialog", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./ManagePowerModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./FirmwareUpdateModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./CoolingModeModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./ManageSecurity", () => ({
  ManageSecurityModal: vi.fn(() => null),
  UpdateMinerPasswordModal: vi.fn(() => null),
}));

vi.mock("../BulkActions/UnsupportedMinersModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("../BulkActions/BulkActionConfirmDialog", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./AddToGroupModal", () => ({
  default: vi.fn(() => null),
}));

describe("SingleMinerActionsMenu", () => {
  it("renders 'View miner' menu item when popover is open", () => {
    render(<SingleMinerActionsMenu deviceIdentifier="test-device-123" />);

    // Open the popover by clicking the trigger button
    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

    // The "View miner" action should be rendered in the popover
    expect(screen.getByText("View miner")).toBeInTheDocument();
    expect(screen.getByTestId("viewMiner-popover-button")).toBeInTheDocument();
  });

  it("navigates to /miners/{deviceIdentifier} when 'View miner' is clicked", () => {
    const deviceIdentifier = "my-device-abc";
    render(<SingleMinerActionsMenu deviceIdentifier={deviceIdentifier} />);

    // Open the popover
    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

    // Click the "View miner" action
    fireEvent.click(screen.getByTestId("viewMiner-popover-button"));

    expect(mockNavigate).toHaveBeenCalledWith(`/miners/${encodeURIComponent(deviceIdentifier)}`);
  });
});
