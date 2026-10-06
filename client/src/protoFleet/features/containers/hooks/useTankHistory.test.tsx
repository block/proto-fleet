import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { useTankHistory } from "./useTankHistory";
import { MeasurementType, MetricSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import { fleetDurations, getFleetDurationMs } from "@/shared/components/DurationSelector";

const api = vi.hoisted(() => ({ getTankHistory: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ fleetInventoryClient: api }));
vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: ({ onError }: { onError: () => void }) => onError() }),
}));
const metric = create(MetricSchema, { openTime: { seconds: 90n }, measurementType: MeasurementType.HASHRATE });
beforeEach(() => {
  vi.resetAllMocks();
  api.getTankHistory.mockResolvedValue({ metrics: [metric] });
});

describe("server-aggregated tank history", () => {
  it.each(fleetDurations)("requests %s in one bounded query and preserves server gaps", async (duration) => {
    const { result } = renderHook(() => useTankHistory("ct1", 81n, duration));
    await waitFor(() => expect(result.current.metrics).toEqual([metric]));
    expect(api.getTankHistory).toHaveBeenCalledTimes(1);
    const [request] = api.getTankHistory.mock.calls[0];
    expect(request).toMatchObject({
      containerIdentifier: "ct1",
      tankId: 81n,
      granularitySeconds: getGranularityForDuration(duration),
    });
    expect(Number(request.endTime.seconds - request.startTime.seconds) * 1000).toBe(getFleetDurationMs(duration));
    expect(result.current.metrics![0].aggregatedValues).toEqual([]);
  });

  it("clears history on permission loss and allows retry", async () => {
    const { result } = renderHook(() => useTankHistory("ct1", 81n, "1h"));
    await waitFor(() => expect(result.current.metrics).toHaveLength(1));
    api.getTankHistory.mockRejectedValueOnce(new ConnectError("Access denied", Code.PermissionDenied));
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.error).toContain("Access denied"));
    expect(result.current.metrics).toEqual([]);
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.metrics).toHaveLength(1));
    expect(result.current.error).toBeUndefined();
  });

  it("aborts requests and ignores late responses when the tank or duration changes", async () => {
    let resolveOld!: (value: { metrics: (typeof metric)[] }) => void;
    api.getTankHistory.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    );
    const { result, rerender } = renderHook(({ tankId, duration }) => useTankHistory("ct1", tankId, duration), {
      initialProps: { tankId: 81n, duration: "1h" as "1h" | "1y" },
    });
    const signal = api.getTankHistory.mock.calls[0][1].signal;
    rerender({ tankId: 82n, duration: "1y" });
    expect(signal.aborted).toBe(true);
    await waitFor(() => expect(result.current.metrics).toHaveLength(1));
    await act(async () => resolveOld({ metrics: [] }));
    expect(result.current.metrics).toHaveLength(1);
  });

  it("does not request history before access is resolved", () => {
    const { result, rerender } = renderHook(({ enabled }) => useTankHistory("ct1", 81n, "1h", enabled), {
      initialProps: { enabled: false },
    });
    expect(result.current.metrics).toEqual([]);
    expect(api.getTankHistory).not.toHaveBeenCalled();
    rerender({ enabled: true });
    expect(api.getTankHistory).toHaveBeenCalledTimes(1);
  });
});
