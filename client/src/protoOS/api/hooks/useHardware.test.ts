import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useHardware } from "./useHardware";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const mockGetHardware = vi.fn();

const {
  mockAddFan,
  mockAddHashboard,
  mockAddPsu,
  mockAuthRetry,
  mockGetHashboard,
  mockGetMiner,
  mockSetControlBoard,
  mockSetMiner,
  mockUseAuthRetry,
  mockUpdateFanTelemetry,
} = vi.hoisted(() => ({
  mockAddFan: vi.fn(),
  mockAddHashboard: vi.fn(),
  mockAddPsu: vi.fn(),
  mockAuthRetry: vi.fn(),
  mockGetHashboard: vi.fn(),
  mockGetMiner: vi.fn(),
  mockSetControlBoard: vi.fn(),
  mockSetMiner: vi.fn(),
  mockUseAuthRetry: vi.fn(),
  mockUpdateFanTelemetry: vi.fn(),
}));

const mockAuthParams = {
  headers: {
    Authorization: "Bearer old-firmware-token",
  },
  secure: false,
};

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({
  useAuthRetry: mockUseAuthRetry,
}));

vi.mock("@/protoOS/store", () => ({
  useMinerStore: {
    getState: () => ({
      hardware: {
        addFan: mockAddFan,
        addHashboard: mockAddHashboard,
        addPsu: mockAddPsu,
        getHashboard: mockGetHashboard,
        getMiner: mockGetMiner,
        setControlBoard: mockSetControlBoard,
        setMiner: mockSetMiner,
      },
      telemetry: {
        updateFanTelemetry: mockUpdateFanTelemetry,
      },
    }),
  },
}));

describe("useHardware", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetHardware.mockResolvedValue({
      data: {
        "hardware-info": {
          "cb-info": {
            machine_name: "Rig",
            board_id: "CB-001",
            serial_number: "SN12345678",
          },
          "hashboards-info": [],
          "psus-info": [],
          "fans-info": [],
        },
      },
    });
    (useMinerHosting as Mock).mockReturnValue({
      api: {
        getHardware: mockGetHardware,
      },
    });
    mockUseAuthRetry.mockReturnValue(mockAuthRetry);
    mockAuthRetry.mockImplementation(({ request, onSuccess }) =>
      request(mockAuthParams).then((result: unknown) => onSuccess?.(result)),
    );
  });

  test("fetches hardware info with auth-compatible params for old firmware", async () => {
    renderHook(() => useHardware());

    await waitFor(() => {
      expect(mockGetHardware).toHaveBeenCalledTimes(1);
    });
    expect(mockGetHardware).toHaveBeenCalledWith(mockAuthParams);
  });

  test("surfaces hardware API error message", async () => {
    mockAuthRetry.mockImplementationOnce(({ onError }) => {
      onError?.({ error: { message: "Hardware unavailable" } });
      return Promise.resolve();
    });

    const { result } = renderHook(() => useHardware());

    await waitFor(() => {
      expect(result.current.error).toBe("Hardware unavailable");
      expect(result.current.pending).toBe(false);
    });
  });

  test("falls back when hardware API error has no message", async () => {
    mockAuthRetry.mockImplementationOnce(({ onError }) => {
      onError?.({});
      return Promise.resolve();
    });

    const { result } = renderHook(() => useHardware());

    await waitFor(() => {
      expect(result.current.error).toBe("An error occurred");
      expect(result.current.pending).toBe(false);
    });
  });
});
