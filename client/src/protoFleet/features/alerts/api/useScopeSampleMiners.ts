import { useEffect, useState } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useSites } from "@/protoFleet/api/sites";
import type { UseAlertScopeResult } from "@/protoFleet/features/alerts/api/useAlertScope";

export const SCOPE_SAMPLE_SIZE = 5;

// Enough headroom that a cross-dimension overlap still fills the sample.
const PER_FILTER_PAGE = 25;
// Device-only scopes have no server-side id filter; match ids against one generous page.
const DEVICE_MATCH_PAGE = 200;

export interface ScopeSampleResult {
  sample: MinerStateSnapshot[];
  // Exact server total when the scope is expressible as one list filter; null when
  // the sample was merged across dimensions (union totals would double-count).
  total: number | null;
  loading: boolean;
}

// Sample of miners the scope currently covers, for the preview pane. MinerListFilter
// facets AND server-side, so each dimension fetches separately and merges to keep union semantics.
export function useScopeSampleMiners(enabled: boolean, scope: UseAlertScopeResult): ScopeSampleResult {
  const { listSites } = useSites();
  const [sample, setSample] = useState<MinerStateSnapshot[]>([]);
  const [total, setTotal] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);

  const scopeKey = JSON.stringify({
    a: scope.allSites,
    s: scope.siteIds,
    b: scope.buildingIds,
    r: scope.rackIds,
    g: scope.groupIds,
    d: scope.deviceIds,
  });

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;

    const resolveAllSiteIds = () =>
      new Promise<string[]>((resolve) => {
        void listSites({
          onSuccess: (rows) => resolve(rows.map((row) => (row.site?.id ?? 0n).toString()).filter((id) => id !== "0")),
          onError: () => resolve([]),
        });
      });

    const run = async () => {
      setLoading(true);
      try {
        const filters: { siteIds?: bigint[]; buildingIds?: bigint[]; rackIds?: bigint[]; groupIds?: bigint[] }[] = [];
        if (scope.allSites) {
          const siteIds = await resolveAllSiteIds();
          if (siteIds.length > 0) filters.push({ siteIds: siteIds.map(BigInt) });
        } else if (scope.siteIds.length > 0) {
          filters.push({ siteIds: scope.siteIds.map(BigInt) });
        }
        if (scope.buildingIds.length > 0) filters.push({ buildingIds: scope.buildingIds.map(BigInt) });
        if (scope.rackIds.length > 0) filters.push({ rackIds: scope.rackIds.map(BigInt) });
        if (scope.groupIds.length > 0) filters.push({ groupIds: scope.groupIds.map(BigInt) });

        if (scope.isOrgWide) filters.push({});

        // Concurrent so a multi-dimension scope costs one round trip of latency, not one per dimension.
        const filterFetches = filters.map((filter) =>
          fleetManagementClient.listMinerStateSnapshots({ pageSize: PER_FILTER_PAGE, filter }),
        );
        // Explicitly picked miners are part of the union whatever else is selected;
        // there is no server-side id filter, so match them against one generous page.
        const deviceFetch =
          scope.deviceIds.length > 0 && !scope.isOrgWide
            ? fleetManagementClient.listMinerStateSnapshots({ pageSize: DEVICE_MATCH_PAGE })
            : null;
        const [filterResults, deviceResult] = await Promise.all([Promise.all(filterFetches), deviceFetch]);

        const byId = new Map<string, MinerStateSnapshot>();
        let exactTotal: number | null = null;
        for (const res of filterResults) {
          for (const miner of res.miners) byId.set(miner.deviceIdentifier, miner);
          // Union totals across dimensions would double-count, and explicit device
          // ids can be stale after edits; only a single-filter scope has an exact total.
          if (filters.length === 1 && scope.deviceIds.length === 0) exactTotal = res.totalMiners;
        }
        if (deviceResult) {
          const wanted = new Set(scope.deviceIds);
          for (const miner of deviceResult.miners) {
            if (wanted.has(miner.deviceIdentifier)) byId.set(miner.deviceIdentifier, miner);
          }
        }
        if (cancelled) return;
        setSample([...byId.values()].slice(0, SCOPE_SAMPLE_SIZE));
        setTotal(exactTotal);
      } catch {
        // Preview only: a failed sample never blocks the form; the summary text still renders.
        if (!cancelled) {
          setSample([]);
          setTotal(null);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void run();

    return () => {
      cancelled = true;
    };
    // scopeKey captures every scope field the fetch reads.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, scopeKey, listSites]);

  return { sample, total, loading };
}
