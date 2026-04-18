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
});
