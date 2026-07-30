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

const mockGetStoreState = vi.fn();
vi.mock("@/protoOS/store/useMinerStore", () => ({
  default: {
    getState: () => mockGetStoreState(),
  },
}));

describe("useSystemStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    currentOnboarded = undefined;
    currentPasswordSet = undefined;
    currentDefaultPasswordActive = undefined;

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

    mockGetStoreState.mockImplementation(() => ({
      minerStatus: { defaultPasswordActive: currentDefaultPasswordActive },
    }));

    mockGetSystemStatus.mockResolvedValue({
      data: {
        onboarded: true,
        password_set: true,
      },
    });
  });

  test("stores onboarded and password_set from the status response", async () => {
    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockGetSystemStatus).toHaveBeenCalledWith({ secure: false });
    });

    expect(mockSetOnboarded).toHaveBeenCalledWith(true);
    expect(mockSetPasswordSet).toHaveBeenCalledWith(true);
  });

  test("resolves an undefined defaultPasswordActive to false on status load", async () => {
    currentDefaultPasswordActive = undefined;

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(false);
    });
  });

  test("does not clear a defaultPasswordActive flag raised by the 403 contract", async () => {
    // The status endpoint no longer reports default_password_active, so a
    // flag raised by a 403 default-password error must survive status polls;
    // only the password-change flow clears it.
    currentDefaultPasswordActive = true;

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockGetSystemStatus).toHaveBeenCalled();
    });

    expect(mockSetDefaultPasswordActive).not.toHaveBeenCalled();
  });

  test("does not re-resolve a defaultPasswordActive flag already cleared", async () => {
    currentDefaultPasswordActive = false;

    renderHook(() => useSystemStatus());

    await waitFor(() => {
      expect(mockGetSystemStatus).toHaveBeenCalled();
    });

    expect(mockSetDefaultPasswordActive).not.toHaveBeenCalled();
  });

  test("stops polling once status has loaded", () => {
    currentOnboarded = true;
    currentPasswordSet = true;

    renderHook(() => useSystemStatus());

    expect(usePoll).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }));
    expect(mockGetSystemStatus).not.toHaveBeenCalled();
  });
});
