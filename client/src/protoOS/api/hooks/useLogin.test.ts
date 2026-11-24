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
      adapter: {
        login: mockLogin,
      },
    });
    (useSetAuthTokens as Mock).mockReturnValue(mockSetAuthTokens);
  });

  test("calls the public login endpoint with secure false and stores tokens", async () => {
    mockLogin.mockResolvedValue({
      accessToken: "new-access-token",
      refreshToken: "new-refresh-token",
    });

    const { result } = renderHook(() => useLogin());

    await result.current({
      password: "admin-password",
    });

    expect(mockLogin).toHaveBeenCalledWith("admin-password");
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
