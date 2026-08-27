import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import { useFirmwareUpdate } from "./useFirmwareUpdate";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AUTH_ACTIONS } from "@/protoOS/store/types";

const { mockCheckAccess, mockSetPausedAuthAction, mockState } = vi.hoisted(() => ({
  mockCheckAccess: vi.fn(),
  mockSetPausedAuthAction: vi.fn(),
  mockState: {
    hasAccess: undefined as boolean | undefined,
    pausedAuthAction: null as string | null,
  },
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", async () => {
  const { AUTH_ACTIONS: actions } = await import("@/protoOS/store/types");
  return {
    AUTH_ACTIONS: actions,
    useAccessToken: vi.fn(() => ({ checkAccess: mockCheckAccess, hasAccess: mockState.hasAccess })),
    usePausedAuthAction: vi.fn(() => mockState.pausedAuthAction),
    useSetPausedAuthAction: vi.fn(() => mockSetPausedAuthAction),
  };
});

describe("useFirmwareUpdate", () => {
  const mockStartFirmwareUpdate = vi.fn();
  const mockCheckForFirmwareUpdate = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockState.hasAccess = undefined;
    mockState.pausedAuthAction = null;
    (useMinerHosting as Mock).mockReturnValue({
      adapter: {
        startFirmwareUpdate: mockStartFirmwareUpdate,
        checkForFirmwareUpdate: mockCheckForFirmwareUpdate,
      },
      mode: "direct",
      isFleetHosted: false,
    });
  });

  test("direct mode pauses on the auth gate instead of updating immediately", async () => {
    const { result } = renderHook(() => useFirmwareUpdate());

    await act(async () => {
      await result.current.updateFirmware();
    });

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(AUTH_ACTIONS.update);
    expect(mockCheckAccess).toHaveBeenCalledTimes(1);
    expect(mockStartFirmwareUpdate).not.toHaveBeenCalled();
  });

  test("fleet-hosted mode starts the update directly without the auth gate", async () => {
    (useMinerHosting as Mock).mockReturnValue({
      adapter: {
        startFirmwareUpdate: mockStartFirmwareUpdate,
        checkForFirmwareUpdate: mockCheckForFirmwareUpdate,
      },
      mode: "fleet",
      isFleetHosted: true,
    });
    mockStartFirmwareUpdate.mockResolvedValue(undefined);

    const { result } = renderHook(() => useFirmwareUpdate());

    await act(async () => {
      await result.current.updateFirmware();
    });

    expect(mockStartFirmwareUpdate).toHaveBeenCalledTimes(1);
    expect(mockSetPausedAuthAction).not.toHaveBeenCalledWith(AUTH_ACTIONS.update);
    expect(mockCheckAccess).not.toHaveBeenCalled();
  });

  test("direct mode resumes the update after the auth gate grants access", async () => {
    mockStartFirmwareUpdate.mockResolvedValue(undefined);
    const { rerender } = renderHook(() => useFirmwareUpdate());

    mockState.hasAccess = true;
    mockState.pausedAuthAction = AUTH_ACTIONS.update;
    rerender();

    await waitFor(() => expect(mockStartFirmwareUpdate).toHaveBeenCalledTimes(1));
    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(null);
  });

  test("checks for firmware updates through the adapter", async () => {
    mockCheckForFirmwareUpdate.mockResolvedValue(undefined);
    const { result } = renderHook(() => useFirmwareUpdate());

    await act(async () => {
      await result.current.checkFirmwareUpdate();
    });

    expect(mockCheckForFirmwareUpdate).toHaveBeenCalledTimes(1);
  });
});
