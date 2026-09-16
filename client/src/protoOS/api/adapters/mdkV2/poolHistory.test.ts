import { describe, expect, it, vi } from "vitest";
import type { PoolHistoryRequest } from "../poolHistory";
import { V2HistorySource } from "./generated/generatedApi";
import { MdkV2Adapter, type MdkV2Client } from "./MdkV2Adapter";
import { normalizePoolHistory, queryPoolHistory } from "./poolHistory";
import { asPoolId } from "@/protoOS/store/contract";

const request: PoolHistoryRequest = {
  poolIds: [asPoolId("pool:1"), asPoolId("pool:2")],
  startTime: "2026-09-16T00:00:00Z",
  endTime: "2026-09-16T02:00:00Z",
  intervalMs: 3_600_000,
};
const bucket = (hour: number, accepted: string, rejected: string) => ({
  time: `2026-09-16T${String(hour).padStart(2, "0")}:00:00Z`,
  accepted,
  rejected,
  sampleCount: "1",
  reportingCoverage: 1,
});

describe("pool history", () => {
  it("recognizes complete retained hourly history and actual missing hours", () => {
    const day = { ...request, endTime: "2026-09-17T00:00:00Z" };
    const buckets = Array.from({ length: 24 }, (_, hour) => bucket(hour, "9", "1"));
    expect(normalizePoolHistory({ source: V2HistorySource.HISTORY_SOURCE_HOURLY, buckets }, day)).toEqual({
      accepted: 216,
      rejected: 24,
      acceptanceRate: 0.9,
      partial: false,
    });
    expect(normalizePoolHistory({ buckets: buckets.filter((_, hour) => hour !== 12) }, day).partial).toBe(true);
  });

  it.each([
    { ...request, intervalMs: 900_000 },
    { ...request, startTime: "2026-09-16T00:15:00Z" },
    { ...request, endTime: "2026-09-16T02:15:00Z" },
  ])("rejects requests the retained-history endpoint cannot serve", async (invalid) => {
    const query = vi.fn();
    await expect(
      queryPoolHistory({ telemetryServiceQueryShareQualityHistory: query } as unknown as MdkV2Client, invalid, {}),
    ).rejects.toThrow("Invalid pool history");
    expect(query).not.toHaveBeenCalled();
  });
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
        bucketSize: "BUCKET_SIZE_1H",
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

  it.each([{ status: 503 }, new TypeError("Failed to fetch")])(
    "rejects total outages instead of reporting successful empty data",
    async (error) => {
      const query = vi.fn().mockRejectedValue(error);
      const client = { telemetryServiceQueryShareQualityHistory: query } as unknown as MdkV2Client;
      await expect(queryPoolHistory(client, request, {})).rejects.toThrow("Could not refresh any pool history");
      query.mockResolvedValueOnce({ data: { buckets: [bucket(0, "9", "1")] } });
      const partial = await queryPoolHistory(client, request, {});
      expect(partial.byPool[asPoolId("pool:1")]?.accepted).toBe(9);
      expect(partial.unavailable).toEqual(["pool:2"]);
    },
  );

  it.each([404, 405, 501])("keeps unsupported endpoints unavailable for HTTP %s", async (status) => {
    const query = vi.fn().mockRejectedValue({ status });
    const result = await queryPoolHistory(
      { telemetryServiceQueryShareQualityHistory: query } as unknown as MdkV2Client,
      request,
      {},
    );
    expect(result).toEqual({ byPool: {}, unavailable: request.poolIds });
  });

  it("drains siblings and skips queued requests before an auth retry", async () => {
    let active = 0,
      peak = 0;
    const pending = Array.from({ length: 3 }, () => {
      let resolve!: (value: { data: { buckets: ReturnType<typeof bucket>[] } }) => void;
      let reject!: (error: unknown) => void;
      const promise = new Promise<{ data: { buckets: ReturnType<typeof bucket>[] } }>((yes, no) => {
        resolve = yes;
        reject = no;
      });
      return { promise, resolve, reject };
    });
    let calls = 0;
    const query = vi.fn(() => {
      active++;
      peak = Math.max(peak, active);
      const promise = pending[calls++]?.promise ?? Promise.resolve({ data: { buckets: [] } });
      return promise.finally(() => {
        active--;
      });
    });
    const client = { telemetryServiceQueryShareQualityHistory: query } as unknown as MdkV2Client;
    const fourPools = { ...request, poolIds: [1, 2, 3, 4].map((id) => asPoolId(`pool:${id}`)) };
    const result = queryPoolHistory(client, fourPools, {}).catch(() => queryPoolHistory(client, fourPools, {}));
    pending[0].reject({ status: 401 });
    await vi.waitFor(() => expect(active).toBe(2));
    expect(query).toHaveBeenCalledTimes(3);
    pending[1].resolve({ data: { buckets: [] } });
    await vi.waitFor(() => expect(active).toBe(1));
    expect(query).toHaveBeenCalledTimes(3);
    pending[2].resolve({ data: { buckets: [] } });
    await result;
    expect(query).toHaveBeenCalledTimes(7);
    expect(peak).toBe(3);
    expect(active).toBe(0);
  });
});
