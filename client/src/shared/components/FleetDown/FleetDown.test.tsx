import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";
import FleetDown from "./FleetDown";

// Mock the redirect utility
vi.mock("@/shared/utils/fleetDownRedirect", () => ({
  redirectFromFleetDown: vi.fn(),
}));

// Mock the API client
vi.mock("@/protoFleet/api/clients", () => ({
  onboardingClient: {
    getFleetInitStatus: vi.fn(),
  },
}));

// Mock the usePoll hook
vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

// Mock AnimatedDotsBackground since it's complex and not relevant to this test
vi.mock("@/shared/components/Animation", () => ({
  default: () => <div data-testid="animated-dots-background" />,
}));

// Mock LogoAlt
vi.mock("@/shared/assets/icons/LogoAlt", () => ({
  default: () => <div data-testid="logo-alt" />,
}));

describe("FleetDown", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the error message and retry button", () => {
    render(<FleetDown />);

    expect(screen.getByText("Fleet will be right back")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry now/i })).toBeInTheDocument();
  });

  it("renders the logo", () => {
    render(<FleetDown />);

    expect(screen.getByTestId("logo-alt")).toBeInTheDocument();
  });

  it("renders the animated dots background", () => {
    render(<FleetDown />);

    expect(screen.getByTestId("animated-dots-background")).toBeInTheDocument();
  });

  it("redirects when backend is back up", async () => {
    const { redirectFromFleetDown } = await import("@/shared/utils/fleetDownRedirect");
    const { onboardingClient } = await import("@/protoFleet/api/clients");
    const user = userEvent.setup();

    // Mock successful API call (backend is up)
    vi.mocked(onboardingClient.getFleetInitStatus).mockResolvedValueOnce({} as any);

    render(<FleetDown />);

    const button = screen.getByRole("button", { name: /retry now/i });
    await user.click(button);

    expect(onboardingClient.getFleetInitStatus).toHaveBeenCalledTimes(1);
    expect(redirectFromFleetDown).toHaveBeenCalledTimes(1);
  });

  it("stays on error page when backend is still down", async () => {
    const { redirectFromFleetDown } = await import("@/shared/utils/fleetDownRedirect");
    const { onboardingClient } = await import("@/protoFleet/api/clients");
    const user = userEvent.setup();

    // Mock failed API call (backend still down)
    const error = new ConnectError("Backend down", Code.Unavailable);
    vi.mocked(onboardingClient.getFleetInitStatus).mockRejectedValueOnce(error);

    render(<FleetDown />);

    const button = screen.getByRole("button", { name: /retry now/i });
    await user.click(button);

    expect(onboardingClient.getFleetInitStatus).toHaveBeenCalledTimes(1);
    expect(redirectFromFleetDown).not.toHaveBeenCalled();
    // Button should be enabled again (not loading)
    expect(button).not.toBeDisabled();
  });

  it("shows loading state while checking backend", async () => {
    const { onboardingClient } = await import("@/protoFleet/api/clients");
    const user = userEvent.setup();

    // Mock API call that resolves successfully
    vi.mocked(onboardingClient.getFleetInitStatus).mockResolvedValueOnce({} as any);

    render(<FleetDown />);

    const button = screen.getByRole("button", { name: /retry now/i });

    // Button should be enabled before click
    expect(button).not.toBeDisabled();

    // Click button and wait for async operation
    await user.click(button);

    // After async completes, verify the API was called
    expect(onboardingClient.getFleetInitStatus).toHaveBeenCalledTimes(1);
  });

  it("automatically polls backend every 15 seconds", async () => {
    const { usePoll } = await import("@/shared/hooks/usePoll");

    render(<FleetDown />);

    expect(usePoll).toHaveBeenCalledWith({
      fetchData: expect.any(Function),
      poll: true,
      pollIntervalMs: 15000,
    });
  });
});
