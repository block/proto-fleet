import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useRefresh } from "./useRefresh";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore, useSetAuthTokens } from "@/protoOS/store";

const mockRefreshToken = vi.fn();
const mockSetAuthTokens = vi.fn();

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  __esModule: true,
  useMinerStore: {
    getState: vi.fn(),
  },
  useSetAuthTokens: vi.fn(),
}));

describe("useRefresh", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    (useMinerHosting as Mock).mockReturnValue({
      api: {
        refreshToken: mockRefreshToken,
      },
    });
    (useMinerStore.getState as Mock).mockReturnValue({
      auth: {
        authTokens: {
          accessToken: { value: "stale-access", expiry: new Date("2026-01-01T00:00:00Z") },
          refreshToken: { value: "latest-refresh", expiry: new Date("2026-01-02T00:00:00Z") },
        },
      },
    });
    (useSetAuthTokens as Mock).mockReturnValue(mockSetAuthTokens);
  });

  test("merges the refreshed access token with the latest store auth tokens", async () => {
    mockRefreshToken.mockResolvedValue({
      data: {
        access_token: "new-access-token",
      },
    });

    const { result } = renderHook(() => useRefresh());

    await result.current({
      refreshToken: "latest-refresh",
    });

    expect(mockRefreshToken).toHaveBeenCalledWith({ refresh_token: "latest-refresh" }, { secure: false });
    expect(mockSetAuthTokens).toHaveBeenCalledWith({
      accessToken: {
        value: "new-access-token",
        expiry: expect.any(Date),
      },
      refreshToken: {
        value: "latest-refresh",
        expiry: new Date("2026-01-02T00:00:00Z"),
      },
    });
  });

  test("drops a stale response when the store's refresh token changed mid-flight", async () => {
    // Session A fires /auth/refresh with refresh token "stale-refresh". While
    // that request is in flight, the user logs out + back in as B, and the
    // store now holds "latest-refresh". A's response must not overwrite B's
    // access token — otherwise the session ends up with B's refresh token
    // and A's access token.
    mockRefreshToken.mockResolvedValue({
      data: { access_token: "session-a-access-token" },
    });
    const onSuccess = vi.fn();
    const onError = vi.fn();

    const { result } = renderHook(() => useRefresh());

    await result.current({
      refreshToken: "stale-refresh",
      onSuccess,
      onError,
    });

    expect(mockSetAuthTokens).not.toHaveBeenCalled();
    expect(onSuccess).not.toHaveBeenCalled();
    // onError must still fire (non-401 so useAuthErrors doesn't log out) so
    // the shared refreshPromise in useAuthErrors settles instead of hanging.
    expect(onError).toHaveBeenCalledWith(expect.objectContaining({ status: 0 }));
  });
});
