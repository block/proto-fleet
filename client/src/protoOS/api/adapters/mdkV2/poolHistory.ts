import type { PoolHistoryRequest, PoolHistoryResult, PoolShareStatistics } from "../poolHistory";
import type { AdapterRequestOptions } from "../types";
import { mapWithConcurrency } from "../utils/async";
import type {
  TelemetryServiceQueryShareQualityHistoryParams,
  V2QueryShareQualityHistoryResponse,
} from "./generated/generatedApi";
import type { MdkV2Client } from "./MdkV2Adapter";
import { isAbortError } from "@/shared/utils/abort";

const bucketSizes: Record<number, TelemetryServiceQueryShareQualityHistoryParams["bucketSize"]> = {
  60_000: "BUCKET_SIZE_1M",
  300_000: "BUCKET_SIZE_5M",
  900_000: "BUCKET_SIZE_15M",
  3_600_000: "BUCKET_SIZE_1H",
};

const count = (value: string | undefined) => {
  if (value === undefined || !/^\d+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
};

export function normalizePoolHistory(
  wire: V2QueryShareQualityHistoryResponse,
  request: PoolHistoryRequest,
): PoolShareStatistics {
  const start = Date.parse(request.startTime);
  const end = Date.parse(request.endTime);
  let accepted = 0;
  let rejected = 0;
  let observed = 0;
  let partial = false;
  const times = new Set<number>();
  for (const bucket of wire.buckets ?? []) {
    const time = Date.parse(bucket.time ?? "");
    // Only count buckets inside the requested half-open window, once each.
    if (!Number.isFinite(time) || time < start || time >= end) continue;
    if (times.has(time)) throw new Error("Duplicate pool history bucket");
    times.add(time);
    const yes = count(bucket.accepted);
    const no = count(bucket.rejected);
    if (bucket.sampleCount === "0" || yes === undefined || no === undefined) {
      partial = true;
      continue;
    }
    accepted += yes;
    rejected += no;
    observed++;
    if (bucket.reportingCoverage !== undefined && bucket.reportingCoverage < 1) partial = true;
  }
  if (!Number.isSafeInteger(accepted + rejected)) throw new Error("Pool share counts exceed safe precision");
  if (!observed) return { partial };
  return {
    accepted,
    rejected,
    acceptanceRate: accepted + rejected > 0 ? accepted / (accepted + rejected) : undefined,
    partial: partial || observed < Math.ceil((end - start) / request.intervalMs),
  };
}

export async function queryPoolHistory(
  client: MdkV2Client,
  request: PoolHistoryRequest,
  params: AdapterRequestOptions,
): Promise<PoolHistoryResult> {
  const start = Date.parse(request.startTime);
  const end = Date.parse(request.endTime);
  const bucketSize = bucketSizes[request.intervalMs];
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || !bucketSize)
    throw new Error("Invalid pool history range or interval");
  const byPool: PoolHistoryResult["byPool"] = {};
  const unavailable: PoolHistoryResult["unavailable"] = [];
  await mapWithConcurrency(
    [...new Set(request.poolIds)],
    3,
    async (id) => {
      const wireId = id.replace(/^pool:/, "");
      const poolId = /^\d+$/.test(wireId) ? Number(wireId) : NaN;
      // The history filter is int32, although configured IDs are int64 strings.
      if (!Number.isInteger(poolId) || poolId <= 0 || poolId > 2_147_483_647) {
        unavailable.push(id);
        return;
      }
      try {
        const { data } = await client.telemetryServiceQueryShareQualityHistory(
          {
            "range.start": request.startTime,
            "range.end": request.endTime,
            bucketSize,
            // Explicit filtering avoids joining statistics by mutable/nonunique pool names.
            poolId,
            grouping: "SHARE_QUALITY_HISTORY_GROUPING_TOTAL",
          },
          params,
        );
        byPool[id] = normalizePoolHistory(data, request);
      } catch (error) {
        const status = (error as { status?: number })?.status;
        if (params.signal?.aborted || isAbortError(error) || status === 401 || status === 403) throw error;
        unavailable.push(id);
      }
    },
    params.signal ?? undefined,
  );
  return { byPool, unavailable };
}
