import { describe, expect, it, vi } from "vitest";
import type { PoolHistoryRequest } from "../poolHistory";
import { MdkV2Adapter, type MdkV2Client } from "./MdkV2Adapter";
import { normalizePoolHistory, queryPoolHistory } from "./poolHistory";
import { asPoolId } from "@/protoOS/store/contract";

const request: PoolHistoryRequest = {
  poolIds: [asPoolId("pool:1"), asPoolId("pool:2")],
  startTime: "2026-09-16T00:00:00Z",
  endTime: "2026-09-16T00:02:00Z",
  intervalMs: 60_000,
};
const bucket = (minute: number, accepted: string, rejected: string) => ({
  time: `2026-09-16T00:0${minute}:00Z`,
  accepted,
  rejected,
  sampleCount: "1",
  reportingCoverage: 1,
});

describe("pool history", () => {
  it("derives the acceptance rate from counts, excluding the next bucket boundary", () => {
    expect(
      normalizePoolHistory({ buckets: [bucket(0, "9", "1"), bucket(1, "1", "9"), bucket(2, "900", "0")] }, request),
    ).toEqual({
      accepted: 10,
      rejected: 10,
      acceptanceRate: 0.5,
      partial: false,
    });
    expect(
      normalizePoolHistory({ buckets: [bucket(0, "900", "100"), bucket(1, "0", "10")] }, request).acceptanceRate,
    ).toBe(900 / 1010);
  });

  it("distinguishes no observations from a reported zero and flags missing coverage", () => {
    expect(normalizePoolHistory({ buckets: [] }, request)).toEqual({ partial: false });
    expect(normalizePoolHistory({ buckets: [bucket(0, "0", "0"), bucket(1, "0", "0")] }, request)).toEqual({
      accepted: 0,
      rejected: 0,
      acceptanceRate: undefined,
      partial: false,
    });
    expect(
      normalizePoolHistory({ buckets: [{ ...bucket(0, "4", "1"), reportingCoverage: 0.5 }] }, request),
    ).toMatchObject({ accepted: 4, partial: true });
    expect(
      normalizePoolHistory({ buckets: [{ ...bucket(0, "0", "0"), sampleCount: "0" }] }, request).accepted,
    ).toBeUndefined();
  });

  it("rejects duplicate buckets and unsafe counts instead of inflating totals", () => {
    expect(() => normalizePoolHistory({ buckets: [bucket(0, "1", "0"), bucket(0, "1", "0")] }, request)).toThrow(
      "Duplicate",
    );
    expect(normalizePoolHistory({ buckets: [bucket(0, "9007199254740992", "0")] }, request).accepted).toBeUndefined();
  });

  it.each([{ ...bucket(0, "0", "0"), sampleCount: "0" }, bucket(0, "invalid", "0")])(
    "preserves partial coverage when no buckets are usable",
    (unusable) => {
      expect(normalizePoolHistory({ buckets: [unusable] }, request)).toEqual({ partial: true });
    },
  );

  it("filters each pool by stable ID and forwards request decoration and cancellation", async () => {
    const query = vi.fn().mockResolvedValue({ data: { buckets: [bucket(0, "9", "1")] } });
    const signal = new AbortController().signal;
    const adapter = new MdkV2Adapter({ telemetryServiceQueryShareQualityHistory: query } as unknown as MdkV2Client);
    const result = await adapter.getPoolHistory(request, { signal });
    expect(query).toHaveBeenCalledTimes(2);
    expect(query).toHaveBeenNthCalledWith(
      1,
      {
        "range.start": request.startTime,
        "range.end": request.endTime,
        bucketSize: "BUCKET_SIZE_1M",
        grouping: "SHARE_QUALITY_HISTORY_GROUPING_TOTAL",
        poolId: 1,
      },
      { signal, headers: { "X-Frigate-Actor": "proto-os" } },
    );
    expect(query.mock.calls[1][0].poolId).toBe(2);
    expect(result.byPool[asPoolId("pool:1")]?.accepted).toBe(9);
  });

  it("keeps successful pools when one fails and never truncates oversized IDs", async () => {
    const query = vi
      .fn()
      .mockResolvedValueOnce({ data: { buckets: [bucket(0, "1", "0")] } })
      .mockRejectedValue({ status: 404 });
    const result = await queryPoolHistory(
      { telemetryServiceQueryShareQualityHistory: query } as unknown as MdkV2Client,
      { ...request, poolIds: [...request.poolIds, asPoolId("pool:2147483648")] },
      {},
    );
    expect(query).toHaveBeenCalledTimes(2);
    expect(result.unavailable).toEqual(expect.arrayContaining(["pool:2", "pool:2147483648"]));
    expect(result.byPool[asPoolId("pool:1")]?.accepted).toBe(1);
  });

  it.each([401, 403])("propagates HTTP %s for authentication handling", async (status) => {
    const query = vi.fn().mockRejectedValue({ status });
    await expect(
      queryPoolHistory({ telemetryServiceQueryShareQualityHistory: query } as unknown as MdkV2Client, request, {}),
    ).rejects.toEqual({ status });
  });
});
