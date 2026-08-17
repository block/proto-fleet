import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useHardware } from "./useHardware";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const mockGetHardware = vi.fn();

const { mockAuthRetry, mockUseAuthRetry } = vi.hoisted(() => ({
  mockAuthRetry: vi.fn(),
  mockUseAuthRetry: vi.fn(),
}));

const mockAuthParams = {
  headers: {
    Authorization: "Bearer old-firmware-token",
  },
  secure: false,
};

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({
  useAuthRetry: mockUseAuthRetry,
}));

describe("useHardware", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetHardware.mockResolvedValue({
      hardware: {
        resource: { status: "ready", updatedAtMs: Date.now() },
        controlBoards: new Map(),
        hashboards: new Map(),
        asics: new Map(),
        psus: new Map(),
        fans: new Map(),
      },
      telemetry: {
        resourceByScope: new Map(),
        rollupsByEntity: new Map(),
        hashboards: new Map(),
        asics: new Map(),
        psus: new Map(),
        fans: new Map(),
        coolingModeByOwner: new Map(),
        asicLoadByHashboard: new Map(),
      },
    });
    (useMinerHosting as Mock).mockReturnValue({
      adapter: {
        getHardware: mockGetHardware,
      },
    });
    mockUseAuthRetry.mockReturnValue(mockAuthRetry);
    mockAuthRetry.mockImplementation(({ request, onSuccess }) =>
      request(mockAuthParams).then((result: unknown) => onSuccess?.(result)),
    );
  });

  test("fetches hardware info with auth-compatible params for old firmware", async () => {
    renderHook(() => useHardware());

    await waitFor(() => {
      expect(mockGetHardware).toHaveBeenCalledTimes(1);
    });
    expect(mockGetHardware).toHaveBeenCalledWith({ deviceId: "device:unknown", profile: "unknown" }, mockAuthParams);
  });

  test("surfaces hardware API error message", async () => {
    mockAuthRetry.mockImplementationOnce(({ onError }) => {
      onError?.({ error: { message: "Hardware unavailable" } });
      return Promise.resolve();
    });

    const { result } = renderHook(() => useHardware());

    await waitFor(() => {
      expect(result.current.error).toBe("Hardware unavailable");
      expect(result.current.pending).toBe(false);
    });
  });

  test("falls back when hardware API error has no message", async () => {
    mockAuthRetry.mockImplementationOnce(({ onError }) => {
      onError?.({});
      return Promise.resolve();
    });

    const { result } = renderHook(() => useHardware());

    await waitFor(() => {
      expect(result.current.error).toBe("An error occurred");
      expect(result.current.pending).toBe(false);
    });
  });
});
