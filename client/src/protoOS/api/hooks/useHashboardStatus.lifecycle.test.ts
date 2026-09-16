import { useMemo } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { useHashboardStatus } from "./useHashboardStatus";
import type { AdapterRequestOptions, HashboardStatus } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useHashboardSerialsByBay } from "@/protoOS/store/hooks/useHardware";
import useMinerStore from "@/protoOS/store/useMinerStore";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: vi.fn() }));
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({ handleAuthErrors: vi.fn() }),
}));

const requests: { signal?: AbortSignal | null; resolve: (status: HashboardStatus[]) => void }[] = [];
const getHashboardStatus = vi.fn(
  (_serials: string[], { signal }: AdapterRequestOptions) =>
    new Promise<HashboardStatus[]>((resolve) => requests.push({ signal, resolve })),
);

const status = (serial = "HB-1"): HashboardStatus[] => {
  const id = `serial:${serial}:asic:0`;
  return [
    {
      serial,
      asicIds: [id],
      asics: [{ id, hashboardSerial: serial, index: 0, row: 0, column: 0 }],
      asicTelemetry: [{ asicId: id, temperature: { value: 32, units: "°C" } }],
    },
  ];
};

// Exercise the Diagnostics selection with the real polling hook and store:
// applying ASIC status rebuilds hardware and therefore this derived array.
const useDiagnosticsStatus = (poll: boolean) => {
  const byBay = useHashboardSerialsByBay();
  const serials = useMemo(
    () =>
      Object.values(byBay)
        .flat()
        .filter((serial): serial is string => serial !== null),
    [byBay],
  );
  return useHashboardStatus({ hashboardSerialNumbers: serials, poll });
};

describe("useHashboardStatus request lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    requests.length = 0;
    getHashboardStatus.mockClear();
    vi.mocked(useMinerHosting).mockReturnValue({
      adapter: { getHashboardStatus },
      isFleetHosted: true,
    } as unknown as ReturnType<typeof useMinerHosting>);
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().hardware.addHashboard({ serial: "HB-1", bay: 1, slot: 1 });
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  test("does not refetch after Diagnostics hydrates ASIC hardware with polling disabled", async () => {
    renderHook(() => useDiagnosticsStatus(false));
    expect(getHashboardStatus).toHaveBeenCalledTimes(1);

    await act(async () => requests[0].resolve(status()));
    expect(useMinerStore.getState().hardware.getHashboard("HB-1")?.asicIds).toHaveLength(1);
    expect(getHashboardStatus).toHaveBeenCalledTimes(1);

    act(() => useMinerStore.getState().hardware.addHashboard({ serial: "HB-1", bay: 1, slot: 1 }));
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(getHashboardStatus).toHaveBeenCalledTimes(1);
  });

  test("fetches a changed selection and cancels its previous request", async () => {
    const { rerender, unmount } = renderHook(
      ({ serials }) => useHashboardStatus({ hashboardSerialNumbers: serials, poll: false }),
      { initialProps: { serials: [] as string[] } },
    );
    expect(getHashboardStatus).not.toHaveBeenCalled();

    rerender({ serials: ["HB-1"] });
    expect(getHashboardStatus).toHaveBeenCalledTimes(1);
    rerender({ serials: ["HB-1"] });
    expect(getHashboardStatus).toHaveBeenCalledTimes(1);
    expect(requests[0].signal?.aborted).toBe(false);

    rerender({ serials: ["HB-2"] });
    expect(getHashboardStatus).toHaveBeenCalledTimes(2);
    expect(getHashboardStatus).toHaveBeenLastCalledWith(["HB-2"], expect.anything());
    expect(requests[0].signal?.aborted).toBe(true);

    await act(async () => requests[1].resolve(status("HB-2")));
    expect(getHashboardStatus).toHaveBeenCalledTimes(2);
    unmount();
    expect(requests[1].signal?.aborted).toBe(true);
  });

  test("waits for the polling interval after hardware hydration and stops on unmount", async () => {
    const { unmount } = renderHook(() => useDiagnosticsStatus(true));
    await act(async () => requests[0].resolve(status()));
    expect(getHashboardStatus).toHaveBeenCalledTimes(1);

    await act(() => vi.advanceTimersByTimeAsync(9_999));
    expect(getHashboardStatus).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(getHashboardStatus).toHaveBeenCalledTimes(2);
    await act(async () => requests[1].resolve(status()));
    expect(getHashboardStatus).toHaveBeenCalledTimes(2);

    unmount();
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(getHashboardStatus).toHaveBeenCalledTimes(2);
  });
});
