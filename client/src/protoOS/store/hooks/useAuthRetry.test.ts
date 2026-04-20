import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { useAuthRetry } from "./useAuthRetry";

const mockHandleAuthErrors = vi.fn();
const mockGetState = vi.fn();
let currentAccessToken = "test-token";

const authHeaderFor = (token: string) => ({
  secure: false,
  headers: { Authorization: `Bearer ${token}` },
});

vi.mock("../useMinerStore", () => ({
  default: {
    getState: () => mockGetState(),
  },
}));

vi.mock("./useAuth", () => ({
  useAuthErrors: vi.fn(() => ({ handleAuthErrors: mockHandleAuthErrors })),
}));

describe("useAuthRetry", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    currentAccessToken = "test-token";
    mockGetState.mockImplementation(() => ({
      auth: {
        authTokens: {
          accessToken: { value: currentAccessToken },
        },
      },
    }));
  });

  test("calls request with the latest auth header from the store", async () => {
    const request = vi.fn().mockResolvedValue("result");
    const { result } = renderHook(() => useAuthRetry());
    currentAccessToken = "fresh-token";

    await result.current({ request });

    expect(request).toHaveBeenCalledWith(authHeaderFor("fresh-token"));
  });

  test("calls onSuccess with the request result", async () => {
    const request = vi.fn().mockResolvedValue("result");
    const onSuccess = vi.fn();
    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onSuccess });

    expect(onSuccess).toHaveBeenCalledWith("result");
  });

  test("routes errors through handleAuthErrors", async () => {
    const error = { status: 401, error: { message: "Unauthorized" } };
    const request = vi.fn().mockRejectedValue(error);
    const onError = vi.fn();
    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onError });

    expect(mockHandleAuthErrors).toHaveBeenCalledWith({
      error,
      onError,
      onSuccess: expect.any(Function),
    });
  });

  test("retries with fresh token on successful refresh", async () => {
    const error = { status: 401, error: { message: "Unauthorized" } };
    const request = vi.fn().mockRejectedValueOnce(error).mockResolvedValueOnce("retry-result");
    const onSuccess = vi.fn();

    mockHandleAuthErrors.mockImplementation(({ onSuccess: authOnSuccess }) => {
      return authOnSuccess?.("fresh-token");
    });

    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onSuccess });

    expect(request).toHaveBeenCalledTimes(2);
    expect(request).toHaveBeenNthCalledWith(1, authHeaderFor("test-token"));
    expect(request).toHaveBeenNthCalledWith(2, authHeaderFor("fresh-token"));
    expect(onSuccess).toHaveBeenCalledWith("retry-result");
  });

  test("returns a promise that settles after the full retry chain", async () => {
    const error = { status: 401, error: { message: "Unauthorized" } };
    const request = vi.fn().mockRejectedValueOnce(error).mockResolvedValueOnce(undefined);

    let resolveRefresh!: (value: void | Promise<void>) => void;
    mockHandleAuthErrors.mockImplementation(({ onSuccess: authOnSuccess }) => {
      return new Promise<void>((resolve) => {
        resolveRefresh = () => resolve(authOnSuccess?.("fresh-token"));
      });
    });

    const { result } = renderHook(() => useAuthRetry());

    let settled = false;
    const promise = result.current({ request }).then(() => {
      settled = true;
    });

    // Allow microtasks to run
    await new Promise((r) => setTimeout(r, 0));
    expect(settled).toBe(false);

    resolveRefresh();
    await promise;

    expect(settled).toBe(true);
  });

  test("chains async onSuccess properly", async () => {
    const request = vi.fn().mockResolvedValue("data");
    const order: string[] = [];
    const onSuccess = vi.fn().mockImplementation(async () => {
      await new Promise((r) => setTimeout(r, 10));
      order.push("onSuccess-done");
    });

    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onSuccess });
    order.push("authRetry-done");

    expect(order).toEqual(["onSuccess-done", "authRetry-done"]);
  });

  test("does not call onError on success", async () => {
    const request = vi.fn().mockResolvedValue("result");
    const onError = vi.fn();
    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onError });

    expect(onError).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
  });

  test("does not call onSuccess on error", async () => {
    const error = { status: 500, error: { message: "Server error" } };
    const request = vi.fn().mockRejectedValue(error);
    const onSuccess = vi.fn();

    mockHandleAuthErrors.mockImplementation(({ onError }) => {
      onError?.(error);
    });

    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onSuccess });

    expect(onSuccess).not.toHaveBeenCalled();
  });

  test("skips auth retry when shouldRetry returns false", async () => {
    const error = { status: 401, error: { message: "Password verification error" } };
    const request = vi.fn().mockRejectedValue(error);
    const onError = vi.fn();
    const shouldRetry = vi.fn().mockReturnValue(false);

    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onError, shouldRetry });

    expect(shouldRetry).toHaveBeenCalledWith(error);
    expect(onError).toHaveBeenCalledWith(error);
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
  });

  test("proceeds with auth retry when shouldRetry returns true", async () => {
    const error = { status: 401, error: { message: "Unauthorized" } };
    const request = vi.fn().mockRejectedValue(error);
    const onError = vi.fn();
    const shouldRetry = vi.fn().mockReturnValue(true);

    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onError, shouldRetry });

    expect(shouldRetry).toHaveBeenCalledWith(error);
    expect(mockHandleAuthErrors).toHaveBeenCalledWith({
      error,
      onError,
      onSuccess: expect.any(Function),
    });
  });

  test("does not retry more than once after a successful refresh", async () => {
    const error = { status: 401, error: { message: "Unauthorized" } };
    const request = vi.fn().mockRejectedValue(error);
    const onError = vi.fn();

    mockHandleAuthErrors.mockImplementation(({ onSuccess: authOnSuccess }) => {
      return authOnSuccess?.("fresh-token");
    });

    const { result } = renderHook(() => useAuthRetry());

    await result.current({ request, onError });

    expect(request).toHaveBeenCalledTimes(2);
    expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledWith(error);
  });
});
