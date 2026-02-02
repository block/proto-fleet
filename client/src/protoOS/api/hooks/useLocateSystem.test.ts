import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useLocateSystem } from "./useLocateSystem";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext/useMinerHosting";

vi.mock("@/protoOS/contexts/MinerHostingContext/useMinerHosting", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useAuthErrors: vi.fn(),
  useAuthHeader: vi.fn(),
}));

describe("useLocateSystem", () => {
  const mockLocateSystem = vi.fn();
  const mockHandleAuthErrors = vi.fn();
  const mockAuthHeader = { Authorization: "Bearer test-token" };

  beforeEach(async () => {
    vi.clearAllMocks();

    (useMinerHosting as Mock).mockReturnValue({
      api: {
        locateSystem: mockLocateSystem,
      },
    });

    const mockStore = await import("@/protoOS/store");
    (mockStore.useAuthErrors as Mock).mockReturnValue({
      handleAuthErrors: mockHandleAuthErrors,
    });
    (mockStore.useAuthHeader as Mock).mockReturnValue(mockAuthHeader);
  });

  test("initializes with pending false", () => {
    const { result } = renderHook(() => useLocateSystem());

    expect(result.current.pending).toBe(false);
  });

  test("calls locateSystem API with default ledOnTime", async () => {
    mockLocateSystem.mockResolvedValue(undefined);

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({});

    await waitFor(() => {
      expect(mockLocateSystem).toHaveBeenCalledWith({ led_on_time: 30 }, mockAuthHeader);
    });
  });

  test("calls locateSystem API with custom ledOnTime", async () => {
    mockLocateSystem.mockResolvedValue(undefined);

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({ ledOnTime: 60 });

    await waitFor(() => {
      expect(mockLocateSystem).toHaveBeenCalledWith({ led_on_time: 60 }, mockAuthHeader);
    });
  });

  test("sets pending to true during API call", async () => {
    mockLocateSystem.mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(resolve, 100);
        }),
    );

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({});

    await waitFor(() => {
      expect(result.current.pending).toBe(true);
    });
  });

  test("sets pending to false after successful API call", async () => {
    mockLocateSystem.mockResolvedValue(undefined);

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({});

    await waitFor(() => {
      expect(result.current.pending).toBe(false);
    });
  });

  test("calls onSuccess callback after successful API call", async () => {
    mockLocateSystem.mockResolvedValue(undefined);
    const onSuccess = vi.fn();

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({ onSuccess });

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledTimes(1);
    });
  });

  test("calls handleAuthErrors on API error", async () => {
    const error = new Error("API Error");
    mockLocateSystem.mockRejectedValue(error);

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({});

    await waitFor(() => {
      expect(mockHandleAuthErrors).toHaveBeenCalledWith({
        error,
        onError: undefined,
        onSuccess: expect.any(Function),
      });
    });
  });

  test("calls onError callback through handleAuthErrors", async () => {
    const error = new Error("API Error");
    const onError = vi.fn();

    mockLocateSystem.mockRejectedValue(error);

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({ onError });

    await waitFor(() => {
      expect(mockHandleAuthErrors).toHaveBeenCalledWith({
        error,
        onError,
        onSuccess: expect.any(Function),
      });
    });
  });

  test("does not call API if api is not available", () => {
    (useMinerHosting as Mock).mockReturnValue({
      api: null,
    });

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({});

    expect(mockLocateSystem).not.toHaveBeenCalled();
  });

  test("sets pending to false after API error", async () => {
    mockLocateSystem.mockRejectedValue(new Error("API Error"));

    const { result } = renderHook(() => useLocateSystem());

    result.current.locateSystem({});

    await waitFor(() => {
      expect(result.current.pending).toBe(false);
    });
  });
});
