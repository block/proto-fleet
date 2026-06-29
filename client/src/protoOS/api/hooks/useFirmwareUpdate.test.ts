import { act, renderHook } from "@testing-library/react";
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
    useAuthHeader: vi.fn(() => ({ Authorization: "Bearer test" })),
    usePausedAuthAction: vi.fn(() => mockState.pausedAuthAction),
    useSetPausedAuthAction: vi.fn(() => mockSetPausedAuthAction),
  };
});

describe("useFirmwareUpdate", () => {
  const mockPostUpdateSystem = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockState.hasAccess = undefined;
    mockState.pausedAuthAction = null;
    (useMinerHosting as Mock).mockReturnValue({ api: { postUpdateSystem: mockPostUpdateSystem }, mode: "direct" });
  });

  test("direct mode pauses on the auth gate instead of updating immediately", async () => {
    const { result } = renderHook(() => useFirmwareUpdate());

    await act(async () => {
      await result.current.updateFirmware();
    });

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(AUTH_ACTIONS.update);
    expect(mockCheckAccess).toHaveBeenCalledTimes(1);
    expect(mockPostUpdateSystem).not.toHaveBeenCalled();
  });

  test("fleet-hosted mode posts the update directly without the auth gate", async () => {
    (useMinerHosting as Mock).mockReturnValue({ api: { postUpdateSystem: mockPostUpdateSystem }, mode: "fleet" });
    mockPostUpdateSystem.mockResolvedValue({ status: 200, ok: true });

    const { result } = renderHook(() => useFirmwareUpdate());

    await act(async () => {
      await result.current.updateFirmware();
    });

    expect(mockPostUpdateSystem).toHaveBeenCalledWith({ Authorization: "Bearer test" });
    expect(mockSetPausedAuthAction).not.toHaveBeenCalledWith(AUTH_ACTIONS.update);
    expect(mockCheckAccess).not.toHaveBeenCalled();
  });
});
