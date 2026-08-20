import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useTelemetry } from "./useTelemetry";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetCurrentTelemetry = vi.fn();
let pollFetch: (() => Promise<void>) | undefined;

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: vi.fn(),
}));

vi.mock("@/protoOS/store/storeWriteGuard", () => ({
  beginStoreWrite: vi.fn(() => () => true),
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

describe("useTelemetry", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    pollFetch = undefined;
    mockGetCurrentTelemetry.mockResolvedValue(createEmptyTelemetryState());
    (useMinerHosting as Mock).mockReturnValue({
      adapter: { getCurrentTelemetry: mockGetCurrentTelemetry },
    });
    (useAuthErrors as Mock).mockReturnValue({ handleAuthErrors: vi.fn() });
    (usePoll as Mock).mockImplementation(({ fetchData }) => {
      pollFetch = fetchData;
    });
  });

  test("uses a stable write guard key scoped to the requested telemetry levels", async () => {
    renderHook(() => useTelemetry({ level: ["psu", "miner"], poll: false }));
    await act(async () => {
      await pollFetch?.();
    });

    expect(beginStoreWrite).toHaveBeenCalledWith("telemetry:current:miner,psu");

    renderHook(() => useTelemetry({ level: ["asic"], poll: false }));
    await act(async () => {
      await pollFetch?.();
    });

    expect(beginStoreWrite).toHaveBeenLastCalledWith("telemetry:current:asic");
  });
});
