import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { useAccessToken, useAuthErrors } from "./useAuth";

const mockRefresh = vi.fn();
const mockLogout = vi.fn();
const mockSetShowLoginModal = vi.fn();
const mockUseLocation = vi.hoisted(() => vi.fn());
const mockSetDefaultPasswordActive = vi.fn();
const mockGetState = vi.fn();

vi.mock("@/protoOS/api/hooks/useRefresh", () => ({
  useRefresh: () => mockRefresh,
}));

vi.mock("../useMinerStore", () => ({
  default: Object.assign(
    vi.fn((selector: any) =>
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
        minerStatus: {
          setDefaultPasswordActive: mockSetDefaultPasswordActive,
        },
      }),
    ),
    {
      getState: () => mockGetState(),
    },
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
    mockGetState.mockReturnValue({
      auth: {
        authTokens: {
          refreshToken: { value: "test-refresh-token", expiry: "" },
        },
      },
    });
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

    test("uses the latest refresh token from store when handling 401s", () => {
      mockGetState.mockReturnValue({
        auth: {
          authTokens: {
            refreshToken: { value: "fresh-refresh-token", expiry: "" },
          },
        },
      });

      const { result } = renderHook(() => useAuthErrors());

      result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Unauthorized" } },
      });

      expect(mockRefresh).toHaveBeenCalledWith(
        expect.objectContaining({
          refreshToken: "fresh-refresh-token",
          onError: expect.any(Function),
        }),
      );
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

    test("sets defaultPasswordActive on nested default-password 403 errors", () => {
      const onError = vi.fn();
      const error = {
        status: 403,
        error: {
          error: {
            code: "DEFAULT_PASSWORD_ACTIVE",
            message: "Default password must be changed before accessing this resource",
          },
        },
      };

      const { result } = renderHook(() => useAuthErrors());

      result.current.handleAuthErrors({
        error,
        onError,
      });

      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(true);
      expect(onError).toHaveBeenCalledWith(error);
      expect(mockRefresh).not.toHaveBeenCalled();
    });

    test("does not set defaultPasswordActive for unrelated 403 errors", () => {
      const onError = vi.fn();

      const { result } = renderHook(() => useAuthErrors());

      result.current.handleAuthErrors({
        error: { status: 403, error: { message: "Forbidden" } },
        onError,
      });

      expect(mockSetDefaultPasswordActive).not.toHaveBeenCalled();
      expect(onError).toHaveBeenCalledWith({ status: 403, error: { message: "Forbidden" } });
      expect(mockRefresh).not.toHaveBeenCalled();
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

  test("marks data routes as auth required now that firmware gates reads", () => {
    // Firmware PR #3266 requires auth on every data endpoint, so data pages
    // must prompt login when the user lands on them without valid tokens —
    // otherwise the UI silently 401s on every poll and shows an empty page.
    mockUseLocation.mockReturnValue({ pathname: "/hashrate" });

    const { result } = renderHook(() => useAccessToken(false));

    expect(result.current.routeRequiresAuth).toBe(true);
  });

  test("leaves onboarding routes accessible without auth gating", () => {
    mockUseLocation.mockReturnValue({ pathname: "/onboarding/welcome" });

    const { result } = renderHook(() => useAccessToken(false));

    expect(result.current.routeRequiresAuth).toBe(false);
  });
});
