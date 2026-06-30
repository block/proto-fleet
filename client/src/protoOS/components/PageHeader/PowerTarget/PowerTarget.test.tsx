import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import PowerTarget from "./PowerTarget";
import { useMiningTarget } from "@/protoOS/api/hooks/useMiningTarget";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AUTH_ACTIONS } from "@/protoOS/store/types";
import { PopoverProvider } from "@/shared/components/Popover";

const { mockCheckAccess, mockSetPausedAuthAction, mockState } = vi.hoisted(() => ({
  mockCheckAccess: vi.fn(),
  mockSetPausedAuthAction: vi.fn(),
  mockState: {
    hasAccess: undefined as boolean | undefined,
    pausedAuthAction: null as string | null,
    dismissedLoginModal: false,
  },
}));

// Mock the popover to a single button that fires onUpdateStart with a target —
// the test exercises PowerTarget's auth-gating branch, not the popover UI.
vi.mock("./PowerTargetPopover", () => ({
  default: ({ onUpdateStart }: { onUpdateStart: (target: number) => void }) => (
    <button data-testid="apply-target" onClick={() => onUpdateStart(5000)} />
  ),
}));

vi.mock("@/protoOS/api/hooks/useMiningTarget", () => ({
  useMiningTarget: vi.fn(),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", async () => {
  const { AUTH_ACTIONS: actions } = await import("@/protoOS/store/types");
  return {
    AUTH_ACTIONS: actions,
    useAccessToken: vi.fn(() => ({ checkAccess: mockCheckAccess, hasAccess: mockState.hasAccess })),
    useDismissedLoginModal: vi.fn(() => mockState.dismissedLoginModal),
    useSetDismissedLoginModal: vi.fn(() => vi.fn()),
    usePausedAuthAction: vi.fn(() => mockState.pausedAuthAction),
    useSetPausedAuthAction: vi.fn(() => mockSetPausedAuthAction),
  };
});

describe("PowerTarget", () => {
  const mockUpdateMiningTarget = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockState.hasAccess = undefined;
    mockState.pausedAuthAction = null;
    mockState.dismissedLoginModal = false;
    (useMinerHosting as Mock).mockReturnValue({ mode: "direct", isFleetHosted: false });
    (useMiningTarget as Mock).mockReturnValue({
      miningTarget: 4000,
      defaultTarget: 4000,
      bounds: { min: 1000, max: 6000 },
      pending: false,
      updateMiningTarget: mockUpdateMiningTarget,
      setPending: vi.fn(),
    });
  });

  const openAndApply = () => {
    const { getByTestId } = render(
      <PopoverProvider>
        <PowerTarget />
      </PopoverProvider>,
    );
    fireEvent.click(getByTestId("power-target-widget"));
    fireEvent.click(getByTestId("apply-target"));
  };

  test("direct mode gates the update behind the auth check", () => {
    openAndApply();

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(AUTH_ACTIONS.miningTarget);
    expect(mockCheckAccess).toHaveBeenCalledTimes(1);
    expect(mockUpdateMiningTarget).not.toHaveBeenCalled();
  });

  test("fleet-hosted mode applies the target directly without the auth gate", () => {
    (useMinerHosting as Mock).mockReturnValue({ mode: "fleet", isFleetHosted: true });

    openAndApply();

    expect(mockUpdateMiningTarget).toHaveBeenCalledWith(5000);
    expect(mockSetPausedAuthAction).not.toHaveBeenCalledWith(AUTH_ACTIONS.miningTarget);
    expect(mockCheckAccess).not.toHaveBeenCalled();
  });
});
