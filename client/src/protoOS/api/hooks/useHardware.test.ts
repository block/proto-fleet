import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useHardware } from "./useHardware";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const mockGetHardware = vi.fn();

const {
  mockAddFan,
  mockAddHashboard,
  mockAddPsu,
  mockGetHashboard,
  mockGetMiner,
  mockSetControlBoard,
  mockSetMiner,
  mockUpdateFanTelemetry,
} = vi.hoisted(() => ({
  mockAddFan: vi.fn(),
  mockAddHashboard: vi.fn(),
  mockAddPsu: vi.fn(),
  mockGetHashboard: vi.fn(),
  mockGetMiner: vi.fn(),
  mockSetControlBoard: vi.fn(),
  mockSetMiner: vi.fn(),
  mockUpdateFanTelemetry: vi.fn(),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
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
  });

  test("fetches public hardware info without auth params", async () => {
    renderHook(() => useHardware());

    await waitFor(() => {
      expect(mockGetHardware).toHaveBeenCalledTimes(1);
    });
    expect(mockGetHardware).toHaveBeenCalledWith();
  });
});
