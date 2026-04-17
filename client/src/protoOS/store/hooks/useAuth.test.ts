import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { useAccessToken, useAuthErrors } from "./useAuth";

const mockRefresh = vi.fn();
const mockLogout = vi.fn();
const mockSetShowLoginModal = vi.fn();
const mockUseLocation = vi.hoisted(() => vi.fn());

vi.mock("@/protoOS/api/hooks/useRefresh", () => ({
  useRefresh: () => mockRefresh,
}));

vi.mock("../useMinerStore", () => ({
  default: vi.fn((selector: any) =>
    selector({
      auth: {
        authTokens: {
          refreshToken: { value: "test-refresh-token", expiry: "" },
          accessToken: { value: "test-access-token", expiry: "" },
        },
        logout: mockLogout,
        setAuthTokens: vi.fn(),
        setLoading: vi.fn(),
      },
      ui: {
        setShowLoginModal: mockSetShowLoginModal,
        pausedAuthAction: null,
      },
    }),
  ),
}));

vi.mock("react-router-dom", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-router-dom")>();

  return {
    ...actual,
    useLocation: mockUseLocation,
  };
});

describe("useAuthErrors", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseLocation.mockReturnValue({ pathname: "/" });
  });

  describe("handleAuthErrors", () => {
    test("returns the promise from refresh on 401 errors", () => {
      const refreshPromise = Promise.resolve();
      mockRefresh.mockReturnValue(refreshPromise);

      const { result } = renderHook(() => useAuthErrors());

      const returnValue = result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Unauthorized" } },
        onSuccess: vi.fn(),
      });

      expect(returnValue).toBe(refreshPromise);
      expect(mockRefresh).toHaveBeenCalledWith({
        refreshToken: "test-refresh-token",
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      });
    });

    test("returns undefined for non-401 errors", () => {
      const onError = vi.fn();

      const { result } = renderHook(() => useAuthErrors());

      const returnValue = result.current.handleAuthErrors({
        error: { status: 500, error: { message: "Server error" } },
        onError,
      });

      expect(returnValue).toBeUndefined();
      expect(mockRefresh).not.toHaveBeenCalled();
      expect(onError).toHaveBeenCalledWith({ status: 500, error: { message: "Server error" } });
    });

    test("calls logout and shows login modal when refresh fails with 401", () => {
      mockRefresh.mockImplementation(({ onError }) => {
        onError?.({ status: 401, error: { message: "Refresh failed" } });
        return Promise.resolve();
      });
      const onError = vi.fn();

      const { result } = renderHook(() => useAuthErrors());

      result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Original error" } },
        onError,
      });

      expect(mockLogout).toHaveBeenCalled();
      expect(mockSetShowLoginModal).toHaveBeenCalledWith(true);
      expect(onError).toHaveBeenCalledWith({ status: 401, error: { message: "Original error" } });
    });

    test("calls onError when refresh fails with non-401 error", () => {
      mockRefresh.mockImplementation(({ onError }) => {
        onError?.({ status: 500, error: { message: "Network error" } });
        return Promise.resolve();
      });
      const onError = vi.fn();

      const { result } = renderHook(() => useAuthErrors());

      result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Original error" } },
        onError,
      });

      expect(mockLogout).not.toHaveBeenCalled();
      expect(mockSetShowLoginModal).not.toHaveBeenCalled();
      expect(onError).toHaveBeenCalledWith({ status: 401, error: { message: "Original error" } });
    });

    test("passes onSuccess through to refresh for token retry", async () => {
      const onSuccess = vi.fn();
      mockRefresh.mockImplementation(({ onSuccess: refreshOnSuccess }) => {
        refreshOnSuccess?.("new-token");
        return Promise.resolve();
      });

      const { result } = renderHook(() => useAuthErrors());

      result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Unauthorized" } },
        onSuccess,
      });

      expect(onSuccess).toHaveBeenCalledWith("new-token");
    });
  });
});

describe("useAccessToken", () => {
  test("marks the mining pools settings route as auth required", () => {
    mockUseLocation.mockReturnValue({ pathname: "/settings/mining-pools" });

    const { result } = renderHook(() => useAccessToken(false));

    expect(result.current.routeRequiresAuth).toBe(true);
  });

  test("treats trailing slashes on auth-required routes as protected", () => {
    mockUseLocation.mockReturnValue({ pathname: "/settings/cooling/" });

    const { result } = renderHook(() => useAccessToken(false));

    expect(result.current.routeRequiresAuth).toBe(true);
  });

  test("treats case-variant auth-required routes as protected", () => {
    mockUseLocation.mockReturnValue({ pathname: "/settings/Mining-Pools" });

    const { result } = renderHook(() => useAccessToken(false));

    expect(result.current.routeRequiresAuth).toBe(true);
  });

  test("leaves non-protected routes accessible without auth gating", () => {
    mockUseLocation.mockReturnValue({ pathname: "/hashrate" });

    const { result } = renderHook(() => useAccessToken(false));

    expect(result.current.routeRequiresAuth).toBe(false);
  });
});
