import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import AuthenticationSettings from "./Auth";
import { MinerModelGroupSchema, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuth } from "@/protoFleet/api/useAuth";
import useDefaultPasswordMiners from "@/protoFleet/api/useDefaultPasswordMiners";
import { useLogin } from "@/protoFleet/api/useLogin";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import { useMinerActions } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import { useHasPermission, useUsername } from "@/protoFleet/store";

vi.mock("@/protoFleet/api/useAuth");
vi.mock("@/protoFleet/api/useDefaultPasswordMiners");
vi.mock("@/protoFleet/api/useLogin");
vi.mock("@/protoFleet/api/useMinerModelGroups");
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions");
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/MinerActionModalStack", () => ({
  default: ({ displayCount }: { displayCount?: number }) => (
    <div data-testid="miner-action-modal-stack" data-display-count={displayCount ?? 0} />
  ),
}));
vi.mock("@/protoFleet/store");
vi.mock("@/shared/features/toaster");

const mockSetPassword = vi.fn();
const mockUpdatePassword = vi.fn();
const mockUpdateUsername = vi.fn();
const mockLogin = vi.fn();
const mockGetMinerModelGroups = vi.fn();
const mockRefetchDefaultPasswordMiners = vi.fn();
const mockSecurityActionHandler = vi.fn();

const totalModelGroups = [
  { model: "Rig", manufacturer: "Proto", count: 202 },
  { model: "Antminer S21", manufacturer: "Bitmain", count: 78 },
];

const defaultPasswordModelGroups = [
  { model: "Rig", manufacturer: "Proto", count: 64 },
  { model: "Antminer S21", manufacturer: "Bitmain", count: 9 },
];

beforeEach(() => {
  vi.clearAllMocks();

  vi.mocked(useAuth).mockReturnValue({
    setPassword: mockSetPassword,
    updatePassword: mockUpdatePassword,
    updateUsername: mockUpdateUsername,
    passwordLastUpdatedAt: null,
  });

  vi.mocked(useLogin).mockReturnValue(mockLogin);
  mockGetMinerModelGroups.mockImplementation(async (filter) =>
    filter?.pairingStatuses?.includes(PairingStatus.DEFAULT_PASSWORD) ? defaultPasswordModelGroups : totalModelGroups,
  );
  vi.mocked(useMinerModelGroups).mockReturnValue({
    getMinerModelGroups: mockGetMinerModelGroups,
  });
  vi.mocked(useDefaultPasswordMiners).mockReturnValue({
    minerIds: [],
    miners: {},
    totalMiners: 64,
    hasMore: false,
    isLoading: false,
    hasInitialLoadCompleted: true,
    loadMore: vi.fn(),
    refetch: mockRefetchDefaultPasswordMiners,
    availableModels: [],
  });
  vi.mocked(useMinerActions).mockReturnValue({
    popoverActions: [{ action: "security", actionHandler: mockSecurityActionHandler }],
  } as unknown as ReturnType<typeof useMinerActions>);
  vi.mocked(useHasPermission).mockReturnValue(true);
  vi.mocked(useUsername).mockReturnValue("testuser");
});

describe("AuthenticationSettings", () => {
  describe("autofocus behavior", () => {
    it("autofocuses the current password field in authenticate step", async () => {
      const { getByTestId, getByLabelText } = render(<AuthenticationSettings />);

      // Click Update button for password (second row)
      const passwordRow = getByTestId("password-row");
      const updateButton = passwordRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      await waitFor(() => {
        const passwordInput = getByLabelText("Password");
        expect(passwordInput).toHaveFocus();
      });
    });

    it("autofocuses the new password field in update password step", async () => {
      mockLogin.mockImplementation(({ onSuccess }) => {
        onSuccess(false);
      });

      const { getByTestId, getByLabelText, getByText } = render(<AuthenticationSettings />);

      // Click Update button for password
      const passwordRow = getByTestId("password-row");
      const updateButton = passwordRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      // Fill password and submit authenticate step
      await waitFor(() => {
        const passwordInput = getByLabelText("Password");
        fireEvent.change(passwordInput, { target: { value: "currentpass" } });
      });

      const confirmButton = getByText("Confirm");
      fireEvent.click(confirmButton);

      // Check new password field has autofocus
      await waitFor(() => {
        const newPasswordInput = getByLabelText("New password");
        expect(newPasswordInput).toHaveFocus();
      });
    });

    it("autofocuses the new username field in update username step", async () => {
      mockLogin.mockImplementation(({ onSuccess }) => {
        onSuccess(false);
      });

      const { getByTestId, getByLabelText, getByText } = render(<AuthenticationSettings />);

      // Click Update button for username (first row)
      const usernameRow = getByTestId("username-row");
      const updateButton = usernameRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      // Fill password and submit authenticate step
      await waitFor(() => {
        const passwordInput = getByLabelText("Password");
        fireEvent.change(passwordInput, { target: { value: "currentpass" } });
      });

      const confirmButton = getByText("Confirm");
      fireEvent.click(confirmButton);

      // Check new username field has autofocus
      await waitFor(() => {
        const newUsernameInput = getByLabelText("New username");
        expect(newUsernameInput).toHaveFocus();
      });
    });
  });

  describe("basic rendering", () => {
    it("renders username and password rows", () => {
      const { getByTestId } = render(<AuthenticationSettings />);

      expect(getByTestId("username-row")).toBeInTheDocument();
      expect(getByTestId("password-row")).toBeInTheDocument();
    });

    it("displays current username", () => {
      const { getByTestId } = render(<AuthenticationSettings />);

      const usernameValue = getByTestId("username-value");
      expect(usernameValue).toHaveTextContent("testuser");
    });

    it("opens modal when clicking Update button", () => {
      const { getByText, getByTestId } = render(<AuthenticationSettings />);

      const usernameRow = getByTestId("username-row");
      const updateButton = usernameRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      expect(getByText("Account password required")).toBeInTheDocument();
    });
  });

  describe("devices", () => {
    it("renders Proto Rig total and default-password counts", async () => {
      render(<AuthenticationSettings />);

      expect(await screen.findByText("64 miners are using default passwords")).toBeInTheDocument();
      expect(screen.getByText("Proto Rig")).toBeInTheDocument();
      expect(screen.getByText("64 with default username/password")).toBeInTheDocument();
      expect(screen.getByText("202 miners")).toBeInTheDocument();
      expect(screen.queryByText("Antminer S21")).not.toBeInTheDocument();
    });

    it("hides the Devices card when no Proto Rig miners use default passwords", async () => {
      mockGetMinerModelGroups.mockImplementation(async (filter) =>
        filter?.pairingStatuses?.includes(PairingStatus.DEFAULT_PASSWORD) ? [] : totalModelGroups,
      );

      render(<AuthenticationSettings />);

      await waitFor(() => {
        expect(screen.queryByText("Devices")).not.toBeInTheDocument();
      });
      expect(screen.queryByText("Proto Rig")).not.toBeInTheDocument();
      expect(screen.queryByText(/using default passwords/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/with default username\/password/i)).not.toBeInTheDocument();
      expect(screen.queryByTestId("default-password-update-button")).not.toBeInTheDocument();
    });

    it("starts the all-default-password security flow from the Devices card", async () => {
      render(<AuthenticationSettings />);

      await screen.findByText("64 miners are using default passwords");
      fireEvent.click(screen.getByTestId("default-password-update-button"));

      await waitFor(() => {
        expect(mockSecurityActionHandler).toHaveBeenCalledTimes(1);
      });
      expect(screen.getByTestId("miner-action-modal-stack")).toHaveAttribute("data-display-count", "64");

      const useMinerActionsCalls = vi.mocked(useMinerActions).mock.calls;
      const lastUseMinerActionsCall = useMinerActionsCalls[useMinerActionsCalls.length - 1]?.[0];
      expect(lastUseMinerActionsCall).toMatchObject({
        selectionMode: "all",
        totalCount: 64,
        currentFilter: expect.objectContaining({
          models: ["Rig"],
          pairingStatuses: [PairingStatus.DEFAULT_PASSWORD],
        }),
      });
      expect(
        lastUseMinerActionsCall?.securityModelGroupFilter?.(
          create(MinerModelGroupSchema, { model: "Rig", manufacturer: "Proto", count: 1 }),
        ),
      ).toBe(true);
      expect(
        lastUseMinerActionsCall?.securityModelGroupFilter?.(
          create(MinerModelGroupSchema, { model: "Rig", manufacturer: "Bitmain", count: 1 }),
        ),
      ).toBe(false);
    });

    it("hides the default-password update button without miner password permission", async () => {
      vi.mocked(useHasPermission).mockReturnValue(false);

      render(<AuthenticationSettings />);

      expect(await screen.findByText("64 miners are using default passwords")).toBeInTheDocument();
      expect(screen.queryByTestId("default-password-update-button")).not.toBeInTheDocument();
      expect(mockSecurityActionHandler).not.toHaveBeenCalled();
    });

    it("refreshes default-password data after the security action completes", async () => {
      render(<AuthenticationSettings />);

      await screen.findByText("64 miners are using default passwords");
      const callsBeforeCompletion = mockGetMinerModelGroups.mock.calls.length;
      const useMinerActionsCalls = vi.mocked(useMinerActions).mock.calls;
      const onActionComplete = useMinerActionsCalls[useMinerActionsCalls.length - 1]?.[0].onActionComplete;

      await act(async () => {
        onActionComplete?.();
      });

      expect(mockRefetchDefaultPasswordMiners).toHaveBeenCalledTimes(1);
      await waitFor(() => {
        expect(mockGetMinerModelGroups).toHaveBeenCalledTimes(callsBeforeCompletion + 2);
      });
    });
  });
});
