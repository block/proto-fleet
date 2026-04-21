import { useLocation } from "react-router-dom";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useSystemStatus } from "./useSystemStatus";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useDefaultPasswordActive,
  useOnboarded,
  usePasswordSet,
  useSetDefaultPasswordActive,
  useSetOnboarded,
  useSetPasswordSet,
} from "@/protoOS/store";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetSystemStatus = vi.fn();
const mockSetOnboarded = vi.fn();
const mockSetPasswordSet = vi.fn();
const mockSetDefaultPasswordActive = vi.fn();
let currentOnboarded: boolean | undefined;
let currentPasswordSet: boolean | undefined;
let currentDefaultPasswordActive: boolean | undefined;

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useOnboarded: vi.fn(),
  usePasswordSet: vi.fn(),
  useDefaultPasswordActive: vi.fn(),
  useSetOnboarded: vi.fn(),
  useSetPasswordSet: vi.fn(),
  useSetDefaultPasswordActive: vi.fn(),
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

vi.mock("react-router-dom", () => ({
  useLocation: vi.fn(),
}));

const mockGetStoreState = vi.fn();
vi.mock("@/protoOS/store/useMinerStore", () => ({
  default: {
    getState: () => mockGetStoreState(),
  },
}));

describe("useSystemStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    currentOnboarded = true;
    currentPasswordSet = true;
    currentDefaultPasswordActive = true;

    (useMinerHosting as Mock).mockReturnValue({
      api: {
        getSystemStatus: mockGetSystemStatus,
      },
    });
    (useOnboarded as Mock).mockImplementation(() => currentOnboarded);
    (usePasswordSet as Mock).mockImplementation(() => currentPasswordSet);
    (useDefaultPasswordActive as Mock).mockImplementation(() => currentDefaultPasswordActive);
    (useSetOnboarded as Mock).mockReturnValue(mockSetOnboarded);
    (useSetPasswordSet as Mock).mockReturnValue(mockSetPasswordSet);
    (useSetDefaultPasswordActive as Mock).mockReturnValue(mockSetDefaultPasswordActive);
    (usePoll as Mock).mockImplementation(({ fetchData, enabled }) => {
      if (enabled) {
        void fetchData();
      }
    });

    // Default: match the hook-scoped values but with no valid access token,
    // so the stale-raise guard doesn't interfere with existing cases.
    mockGetStoreState.mockImplementation(() => ({
      minerStatus: { defaultPasswordActive: currentDefaultPasswordActive },
      auth: { authTokens: { accessToken: { value: "", expiry: new Date(0).toISOString() } } },
    }));
  });

  test("does not clear defaultPasswordActive from status polling on password-change routes", async () => {
    (useLocation as Mock).mockReturnValue({ pathname: "/onboarding/authentication" });
    mockGetSystemStatus.mockResolvedValue({
      data: {
        onboarded: true,
        password_set: true,
        default_password_active: false,
      },
    });

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockGetSystemStatus).toHaveBeenCalledWith({ secure: false });
    });

    expect(mockSetOnboarded).toHaveBeenCalledWith(true);
    expect(mockSetPasswordSet).toHaveBeenCalledWith(true);
    expect(mockSetDefaultPasswordActive).not.toHaveBeenCalledWith(false);
  });

  test("applies cleared defaultPasswordActive status outside password-change routes", async () => {
    (useLocation as Mock).mockReturnValue({ pathname: "/settings/mining-pools" });
    mockGetSystemStatus.mockResolvedValue({
      data: {
        onboarded: true,
        password_set: true,
        default_password_active: false,
      },
    });

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(false);
    });
  });

  test("treats a missing default_password_active field as false", async () => {
    (useLocation as Mock).mockReturnValue({ pathname: "/settings/mining-pools" });
    mockGetSystemStatus.mockResolvedValue({
      data: {
        onboarded: true,
        password_set: true,
      },
    });

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(false);
    });
  });

  test("ignores a null system status payload", async () => {
    (useLocation as Mock).mockReturnValue({ pathname: "/settings/mining-pools" });
    mockGetSystemStatus.mockResolvedValue({
      data: null,
    });

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockGetSystemStatus).toHaveBeenCalledWith({ secure: false });
    });

    expect(mockSetOnboarded).not.toHaveBeenCalled();
    expect(mockSetPasswordSet).not.toHaveBeenCalled();
    expect(mockSetDefaultPasswordActive).not.toHaveBeenCalled();
  });

  test("ignores a stale true response once a valid session has cleared the flag", async () => {
    // Poll fired while the flag was still true; by response time the store
    // has been cleared and a valid session established.
    (useLocation as Mock).mockReturnValue({ pathname: "/" });
    currentDefaultPasswordActive = true;
    mockGetStoreState.mockReturnValue({
      minerStatus: { defaultPasswordActive: false },
      auth: {
        authTokens: {
          accessToken: { value: "valid-token", expiry: new Date(Date.now() + 60_000).toISOString() },
        },
      },
    });
    mockGetSystemStatus.mockResolvedValue({
      data: {
        onboarded: true,
        password_set: true,
        default_password_active: true,
      },
    });

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockGetSystemStatus).toHaveBeenCalled();
    });

    expect(mockSetDefaultPasswordActive).not.toHaveBeenCalledWith(true);
  });

  test("allows a poll-driven clear on password-change routes once the session is valid", async () => {
    // When another client (or a prior successful flow) has already cleared
    // default_password_active server-side and our session is valid, the poll
    // must be allowed to update the store even while the user sits on a
    // password-change route — otherwise App.tsx's redirect traps them there.
    (useLocation as Mock).mockReturnValue({ pathname: "/onboarding/authentication" });
    currentDefaultPasswordActive = true;
    mockGetStoreState.mockReturnValue({
      minerStatus: { defaultPasswordActive: true },
      auth: {
        authTokens: {
          accessToken: { value: "valid-token", expiry: new Date(Date.now() + 60_000).toISOString() },
        },
      },
    });
    mockGetSystemStatus.mockResolvedValue({
      data: {
        onboarded: true,
        password_set: true,
        default_password_active: false,
      },
    });

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(false);
    });
  });
});
