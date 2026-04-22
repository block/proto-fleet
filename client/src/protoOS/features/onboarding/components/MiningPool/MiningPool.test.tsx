import React from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MiningPoolPage from "./MiningPool";

vi.mock("motion/react", () => ({
  motion: {
    div: ({ children, ...props }: React.ComponentProps<"div">) => <div {...props}>{children}</div>,
  },
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

// Mock the navigate function
const mockNavigate = vi.fn();
vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mockNavigate,
}));

// Mock pushToast
const mockPushToast = vi.fn();
vi.mock("@/shared/features/toaster", () => ({
  pushToast: (args: unknown) => mockPushToast(args),
  STATUSES: { error: "error" },
}));

// Mock the API hooks
const mockSetCooling = vi.fn();
const mockUseCoolingStatus = vi.fn();
vi.mock("@/protoOS/api", () => ({
  useCoolingStatus: () => mockUseCoolingStatus(),
}));

// Mock the access token hook
const mockCheckAccess = vi.fn();
const mockUseAccessToken = vi.fn();
vi.mock("@/protoOS/store", () => ({
  useAccessToken: () => mockUseAccessToken(),
}));

// Mock child components
vi.mock("@/protoOS/components/MiningPools", () => {
  const MockMiningPools = ({
    children,
    onChange,
  }: {
    children: React.ReactNode;
    onChange: (pools: unknown[]) => void;
  }) => {
    // Simulate having valid pools filled in
    React.useEffect(() => {
      onChange([
        { url: "stratum+tcp://pool.example.com:3333", username: "user1", password: "pass1" },
        { url: "stratum+tcp://backup.example.com:3333", username: "user2", password: "pass2" },
        { url: "", username: "", password: "" },
      ]);
    }, [onChange]);
    return <div data-testid="mining-pools">{children}</div>;
  };

  return {
    default: MockMiningPools,
    getEmptyPoolsInfo: () => [
      { url: "", username: "", password: "" },
      { url: "", username: "", password: "" },
      { url: "", username: "", password: "" },
    ],
    isValidPool: (pool: { url: string; username: string }) => Boolean(pool.url && pool.username),
  };
});

vi.mock("@/protoOS/components/OnboardingSettingUp", () => ({
  default: () => <div data-testid="setting-up">Setting Up</div>,
}));

vi.mock("@/shared/components/MiningPools/WarnBackupPoolDialog", () => ({
  WarnBackupPoolDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="warn-backup-pool-dialog">Warn Backup Pool Dialog</div> : null,
}));

vi.mock("@/shared/components/MiningPools/WarnDefaultPoolCallout", () => ({
  WarnDefaultPoolCallout: ({ show }: { show: boolean }) =>
    show ? <div data-testid="warn-default-pool-callout">Warn Default Pool Callout</div> : null,
}));

vi.mock("@/protoOS/features/onboarding/components/NoFansDetectedDialog", () => ({
  default: ({
    open,
    onUseAirCooling,
    onConfirmImmersionCooling,
  }: {
    open: boolean;
    onUseAirCooling: () => void;
    onConfirmImmersionCooling: () => void;
  }) =>
    open ? (
      <div data-testid="no-fans-dialog">
        <button onClick={onUseAirCooling}>Use air cooling</button>
        <button onClick={onConfirmImmersionCooling}>Confirm immersion cooling</button>
      </div>
    ) : null,
}));

vi.mock("@/shared/components/Setup", () => ({
  OnboardingLayout: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="onboarding-layout">{children}</div>
  ),
}));

vi.mock("@/shared/components/Animation", () => ({
  default: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/shared/components/Callout", () => ({
  DismissibleCalloutWrapper: () => null,
  intents: { danger: "danger" },
}));

vi.mock("@/shared/assets/icons", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/shared/assets/icons")>();
  return {
    ...actual,
    Alert: () => <div>Alert Icon</div>,
  };
});

vi.mock("@/shared/components/ButtonGroup", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/shared/components/ButtonGroup")>();
  return {
    ...actual,
    default: ({ buttons }: { buttons: Array<{ text: string; onClick: () => void; disabled?: boolean }> }) => (
      <div data-testid="button-group">
        {buttons.map((button, idx) => (
          <button key={idx} onClick={button.onClick} disabled={button.disabled}>
            {button.text}
          </button>
        ))}
      </div>
    ),
  };
});

describe("MiningPoolPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    // Default mocks
    mockUseAccessToken.mockReturnValue({
      checkAccess: mockCheckAccess,
    });

    // Default: fans are running (RPM > 0), so dialog should NOT show
    mockUseCoolingStatus.mockReturnValue({
      data: {
        fans: [
          { slot: 1, rpm: 1200 },
          { slot: 2, rpm: 1150 },
          { slot: 3, rpm: 1180 },
        ],
      },
      setCooling: mockSetCooling,
      loaded: true,
      pending: false,
    });
  });

  it("renders the mining pool page", () => {
    render(<MiningPoolPage />);
    // Basic smoke test to ensure component renders without errors
  });

  describe("Fan detection", () => {
    describe("When all fans have RPM = 0 (no fans connected)", () => {
      beforeEach(() => {
        // All fans report RPM = 0 → no fans connected (can't distinguish disconnected vs dead fan)
        mockUseCoolingStatus.mockReturnValue({
          data: {
            fans: [
              { slot: 1, rpm: 0 },
              { slot: 2, rpm: 0 },
              { slot: 3, rpm: 0 },
            ],
          },
          setCooling: mockSetCooling,
          loaded: true,
          pending: false,
        });
      });

      it("shows dialog when Continue is clicked", async () => {
        render(<MiningPoolPage />);

        const continueButton = screen.getByText("Continue");
        continueButton.click();

        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });
      });

      it("shows dialog when Skip is clicked", async () => {
        render(<MiningPoolPage />);

        const skipButton = screen.getByText("Skip");
        skipButton.click();

        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });
      });
    });

    describe("While cooling status is still loading", () => {
      beforeEach(() => {
        mockUseCoolingStatus.mockReturnValue({
          data: undefined,
          setCooling: mockSetCooling,
          loaded: false,
          pending: false,
        });
      });

      it("disables both actions until cooling status has loaded", () => {
        render(<MiningPoolPage />);

        const continueButton = screen.getByText("Continue");
        const skipButton = screen.getByText("Skip");

        expect(continueButton).toBeDisabled();
        expect(skipButton).toBeDisabled();
        expect(screen.queryByTestId("no-fans-dialog")).not.toBeInTheDocument();
        expect(mockCheckAccess).not.toHaveBeenCalled();
        expect(mockNavigate).not.toHaveBeenCalled();
      });
    });

    describe("When all fans are null (no fans data)", () => {
      beforeEach(() => {
        // All fan entries are null → no fans connected
        mockUseCoolingStatus.mockReturnValue({
          data: {
            fans: [null, null, null],
          },
          setCooling: mockSetCooling,
          loaded: true,
          pending: false,
        });
      });

      it("shows dialog when Continue is clicked", async () => {
        render(<MiningPoolPage />);

        const continueButton = screen.getByText("Continue");
        continueButton.click();

        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });
      });

      it("shows dialog when Skip is clicked", async () => {
        render(<MiningPoolPage />);

        const skipButton = screen.getByText("Skip");
        skipButton.click();

        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });
      });
    });

    describe("When at least one fan has RPM > 0 (fans are connected)", () => {
      beforeEach(() => {
        // At least one fan has RPM > 0 → fans are connected, should NOT show dialog
        mockUseCoolingStatus.mockReturnValue({
          data: {
            fans: [
              { slot: 1, rpm: 0 },
              { slot: 2, rpm: 1200 }, // This fan is running
              { slot: 3, rpm: 0 },
            ],
          },
          setCooling: mockSetCooling,
          loaded: true,
          pending: false,
        });
      });

      it("does NOT show dialog when Continue is clicked", async () => {
        render(<MiningPoolPage />);

        const continueButton = screen.getByText("Continue");
        continueButton.click();

        // Should proceed directly to setup, not show the no-fans dialog
        await waitFor(() => {
          expect(mockCheckAccess).toHaveBeenCalled();
        });

        // Dialog should not appear
        expect(screen.queryByTestId("no-fans-dialog")).not.toBeInTheDocument();
      });

      it("does NOT show dialog when Skip is clicked", async () => {
        render(<MiningPoolPage />);

        const skipButton = screen.getByText("Skip");
        skipButton.click();

        // Should navigate directly, not show the no-fans dialog
        await waitFor(() => {
          expect(mockNavigate).toHaveBeenCalledWith("/");
        });

        // Dialog should not appear
        expect(screen.queryByTestId("no-fans-dialog")).not.toBeInTheDocument();
      });
    });

    describe("Continue flow (when no fans connected)", () => {
      beforeEach(() => {
        // Mock no fans connected for these tests
        mockUseCoolingStatus.mockReturnValue({
          data: {
            fans: [
              { slot: 1, rpm: 0 },
              { slot: 2, rpm: 0 },
              { slot: 3, rpm: 0 },
            ],
          },
          setCooling: mockSetCooling,
          loaded: true,
          pending: false,
        });
      });

      it("proceeds with setup when 'Use air cooling' is clicked", async () => {
        render(<MiningPoolPage />);

        // Click Continue
        const continueButton = screen.getByText("Continue");
        continueButton.click();

        // Wait for dialog to appear
        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });

        // Click "Use air cooling"
        const airCoolingButton = screen.getByText("Use air cooling");
        airCoolingButton.click();

        // Should call checkAccess to proceed with setup
        await waitFor(() => {
          expect(mockCheckAccess).toHaveBeenCalled();
        });

        // Should not navigate
        expect(mockNavigate).not.toHaveBeenCalled();
      });

      it("sets cooling mode and proceeds with setup when 'Confirm immersion cooling' is clicked", async () => {
        // Mock setCooling to call onSuccess
        mockSetCooling.mockImplementation(({ onSuccess }: { onSuccess: () => void }) => {
          onSuccess();
        });

        render(<MiningPoolPage />);

        // Click Continue
        const continueButton = screen.getByText("Continue");
        continueButton.click();

        // Wait for dialog
        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });

        // Click "Confirm immersion cooling"
        const immersionButton = screen.getByText("Confirm immersion cooling");
        immersionButton.click();

        // Should call setCooling
        await waitFor(() => {
          expect(mockSetCooling).toHaveBeenCalledWith(
            expect.objectContaining({
              mode: "Off",
            }),
          );
        });

        // Should proceed with setup after success
        await waitFor(() => {
          expect(mockCheckAccess).toHaveBeenCalled();
        });

        // Should not navigate
        expect(mockNavigate).not.toHaveBeenCalled();
      });

      it("shows error toast and keeps dialog open when cooling mode change fails", async () => {
        // Mock setCooling to call onError
        const errorMessage = "API connection failed";
        mockSetCooling.mockImplementation(
          ({ onError }: { onError: (error: { error: { message: string } }) => void }) => {
            onError({ error: { message: errorMessage } });
          },
        );

        render(<MiningPoolPage />);

        // Click Continue
        const continueButton = screen.getByText("Continue");
        continueButton.click();

        // Wait for dialog
        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });

        // Click "Confirm immersion cooling"
        const immersionButton = screen.getByText("Confirm immersion cooling");
        immersionButton.click();

        // Should show error toast
        await waitFor(() => {
          expect(mockPushToast).toHaveBeenCalledWith({
            message: errorMessage,
            status: "error",
          });
        });

        // Should keep dialog open
        expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();

        // Should not proceed with setup
        expect(mockCheckAccess).not.toHaveBeenCalled();

        // Should not navigate
        expect(mockNavigate).not.toHaveBeenCalled();
      });
    });

    describe("Skip flow (when no fans connected)", () => {
      beforeEach(() => {
        // Mock no fans connected for these tests
        mockUseCoolingStatus.mockReturnValue({
          data: {
            fans: [
              { slot: 1, rpm: 0 },
              { slot: 2, rpm: 0 },
              { slot: 3, rpm: 0 },
            ],
          },
          setCooling: mockSetCooling,
          loaded: true,
          pending: false,
        });
      });

      it("navigates to home when 'Use air cooling' is clicked", async () => {
        render(<MiningPoolPage />);

        // Click Skip
        const skipButton = screen.getByText("Skip");
        skipButton.click();

        // Wait for dialog to appear
        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });

        // Click "Use air cooling"
        const airCoolingButton = screen.getByText("Use air cooling");
        airCoolingButton.click();

        // Should navigate to home
        await waitFor(() => {
          expect(mockNavigate).toHaveBeenCalledWith("/");
        });

        // Should not proceed with setup
        expect(mockCheckAccess).not.toHaveBeenCalled();
      });

      it("sets cooling mode and navigates to home when 'Confirm immersion cooling' is clicked", async () => {
        // Mock setCooling to call onSuccess
        mockSetCooling.mockImplementation(({ onSuccess }: { onSuccess: () => void }) => {
          onSuccess();
        });

        render(<MiningPoolPage />);

        // Click Skip
        const skipButton = screen.getByText("Skip");
        skipButton.click();

        // Wait for dialog
        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });

        // Click "Confirm immersion cooling"
        const immersionButton = screen.getByText("Confirm immersion cooling");
        immersionButton.click();

        // Should call setCooling
        await waitFor(() => {
          expect(mockSetCooling).toHaveBeenCalledWith(
            expect.objectContaining({
              mode: "Off",
            }),
          );
        });

        // Should navigate to home after success
        await waitFor(() => {
          expect(mockNavigate).toHaveBeenCalledWith("/");
        });

        // Should not proceed with setup
        expect(mockCheckAccess).not.toHaveBeenCalled();
      });

      it("shows error toast and keeps dialog open when cooling mode change fails", async () => {
        // Mock setCooling to call onError
        const errorMessage = "API connection failed";
        mockSetCooling.mockImplementation(
          ({ onError }: { onError: (error: { error: { message: string } }) => void }) => {
            onError({ error: { message: errorMessage } });
          },
        );

        render(<MiningPoolPage />);

        // Click Skip
        const skipButton = screen.getByText("Skip");
        skipButton.click();

        // Wait for dialog
        await waitFor(() => {
          expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();
        });

        // Click "Confirm immersion cooling"
        const immersionButton = screen.getByText("Confirm immersion cooling");
        immersionButton.click();

        // Should show error toast
        await waitFor(() => {
          expect(mockPushToast).toHaveBeenCalledWith({
            message: errorMessage,
            status: "error",
          });
        });

        // Should keep dialog open
        expect(screen.getByTestId("no-fans-dialog")).toBeInTheDocument();

        // Should not navigate
        expect(mockNavigate).not.toHaveBeenCalled();

        // Should not proceed with setup
        expect(mockCheckAccess).not.toHaveBeenCalled();
      });
    });
  });
});
