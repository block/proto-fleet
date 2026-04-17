import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useLogin } from "./useLogin";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetAuthTokens } from "@/protoOS/store";

const mockLogin = vi.fn();
const mockSetAuthTokens = vi.fn();

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useSetAuthTokens: vi.fn(),
}));

describe("useLogin", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    (useMinerHosting as Mock).mockReturnValue({
      api: {
        login: mockLogin,
      },
    });
    (useSetAuthTokens as Mock).mockReturnValue(mockSetAuthTokens);
  });

  test("calls the public login endpoint with secure false and stores tokens", async () => {
    mockLogin.mockResolvedValue({
      data: {
        access_token: "new-access-token",
        refresh_token: "new-refresh-token",
      },
    });

    const { result } = renderHook(() => useLogin());

    await result.current({
      password: "admin-password",
    });

    expect(mockLogin).toHaveBeenCalledWith({ password: "admin-password" }, { secure: false });
    expect(mockSetAuthTokens).toHaveBeenCalledWith({
      accessToken: {
        value: "new-access-token",
        expiry: expect.any(Date),
      },
      refreshToken: {
        value: "new-refresh-token",
        expiry: expect.any(Date),
      },
    });
  });
});
