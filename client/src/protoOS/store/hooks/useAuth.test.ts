import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { __resetRefreshInFlightForTest, useAccessToken, useAuthErrors } from "./useAuth";

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
    __resetRefreshInFlightForTest();
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
    test("routes a 401 through refresh with onSuccess/onError wired", async () => {
      mockRefresh.mockImplementation(({ onSuccess }) => {
        onSuccess?.("new-token");
        return Promise.resolve();
      });

      const { result } = renderHook(() => useAuthErrors());

      const onSuccess = vi.fn();
      const returnValue = result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Unauthorized" } },
        onSuccess,
      });

      expect(returnValue).toBeInstanceOf(Promise);
      await returnValue;

      expect(mockRefresh).toHaveBeenCalledWith(
        expect.objectContaining({
          refreshToken: "test-refresh-token",
          onSuccess: expect.any(Function),
          onError: expect.any(Function),
        }),
      );
      expect(onSuccess).toHaveBeenCalledWith("new-token");
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

    test("calls logout and shows login modal when refresh fails with 401", async () => {
      mockRefresh.mockImplementation(({ onError }) => {
        onError?.({ status: 401, error: { message: "Refresh failed" } });
        return Promise.resolve();
      });
      const onError = vi.fn();

      const { result } = renderHook(() => useAuthErrors());

      await result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Original error" } },
        onError,
      });

      expect(mockLogout).toHaveBeenCalled();
      expect(mockSetShowLoginModal).toHaveBeenCalledWith(true);
      expect(onError).toHaveBeenCalledWith({ status: 401, error: { message: "Original error" } });
    });

    test("calls onError when refresh fails with non-401 error", async () => {
      mockRefresh.mockImplementation(({ onError }) => {
        onError?.({ status: 500, error: { message: "Network error" } });
        return Promise.resolve();
      });
      const onError = vi.fn();

      const { result } = renderHook(() => useAuthErrors());

      await result.current.handleAuthErrors({
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

      await result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Unauthorized" } },
        onSuccess,
      });

      expect(onSuccess).toHaveBeenCalledWith("new-token");
    });

    test("a later 401 still refreshes after a stale refresh response settled via onError", async () => {
      // useRefresh drops stale responses (session changed mid-flight) through
      // onError rather than returning silently. If it didn't, the shared
      // refreshPromise would hang forever and this second 401 would await it
      // indefinitely instead of kicking off its own refresh.
      mockRefresh.mockImplementationOnce(({ onError }) => {
        onError?.({ status: 0, error: { message: "refresh response dropped: session changed mid-flight" } });
        return Promise.resolve();
      });
      const staleOnError = vi.fn();
      await renderHook(() => useAuthErrors()).result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Unauthorized" } },
        onError: staleOnError,
      });
      expect(staleOnError).toHaveBeenCalled();

      // Second 401 on a fresh session — if refreshPromise were stuck, this
      // would never resolve.
      mockRefresh.mockImplementationOnce(({ onSuccess }) => {
        onSuccess?.("new-token");
        return Promise.resolve();
      });
      const onSuccess = vi.fn();
      await renderHook(() => useAuthErrors()).result.current.handleAuthErrors({
        error: { status: 401, error: { message: "Unauthorized" } },
        onSuccess,
      });

      expect(onSuccess).toHaveBeenCalledWith("new-token");
    });

    test("all concurrent 401 callers retry with the same refreshed token", async () => {
      let refreshOnSuccess: ((token: string) => void) | undefined;
      let refreshCalls = 0;
      mockRefresh.mockImplementation(({ onSuccess }) => {
        refreshCalls += 1;
        refreshOnSuccess = onSuccess;
        return Promise.resolve();
      });

      const { result } = renderHook(() => useAuthErrors());

      // Three callers all 401 before refresh resolves — e.g. a write and two
      // polling hooks racing on an expired access token.
      const onSuccessA = vi.fn();
      const onSuccessB = vi.fn();
      const onSuccessC = vi.fn();
      const promises = [
        result.current.handleAuthErrors({
          error: { status: 401, error: { message: "Unauthorized" } },
          onSuccess: onSuccessA,
        }),
        result.current.handleAuthErrors({
          error: { status: 401, error: { message: "Unauthorized" } },
          onSuccess: onSuccessB,
        }),
        result.current.handleAuthErrors({
          error: { status: 401, error: { message: "Unauthorized" } },
          onSuccess: onSuccessC,
        }),
      ];

      // Only one /auth/refresh fires despite three concurrent callers.
      expect(refreshCalls).toBe(1);

      refreshOnSuccess?.("shared-new-token");
      await Promise.all(promises);

      expect(onSuccessA).toHaveBeenCalledWith("shared-new-token");
      expect(onSuccessB).toHaveBeenCalledWith("shared-new-token");
      expect(onSuccessC).toHaveBeenCalledWith("shared-new-token");
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
    // Firmware now requires auth on every data endpoint, so data pages must
    // prompt login when the user lands on them without valid tokens — otherwise
    // the UI silently 401s on every poll and shows an empty page.
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
