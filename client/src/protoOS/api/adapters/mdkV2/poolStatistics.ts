import { Code } from "@connectrpc/connect";
import { isAuthenticationError } from "../errors";
import type { PoolConfigurationStatisticsService } from "../poolStatistics";
import type { AdapterRequestOptions } from "../types";
import { mapWithConcurrency } from "../utils/async";
import type { MdkV2Client } from "./connectClient";
import { withDeadline } from "./controllerRecords";
import { groupStamp } from "./miningGroupRecords";
import { toCanonicalPoolRecordedShares, toCanonicalPools } from "./normalize";
import { readDefaultPools } from "./poolEdits";

export function createPoolStatistics(
  client: MdkV2Client,
  decorate: (params?: AdapterRequestOptions) => AdapterRequestOptions,
): PoolConfigurationStatisticsService {
  const read = <T>(
    params: AdapterRequestOptions | undefined,
    request: (options: AdapterRequestOptions) => Promise<T>,
  ) => withDeadline(decorate(params), 10_000, request);
  return {
    list: (params) =>
      read(params, async (options) => {
        const response = await client.miningGroupServiceGetMiningGroupCapability(options).catch((cause: unknown) => {
          const error = cause as { status?: number; error?: { code?: number } };
          if ([404, 405, 501].includes(error.status ?? 0) || error.error?.code === Code.Unimplemented)
            return {
              data: { capability: { name: "mining-groups-v1", version: 1, enabled: false, stateRevision: undefined } },
            };
          throw cause;
        });
        const capability = response.data.capability;
        if (capability?.name !== "mining-groups-v1" || capability.version !== 1)
          throw new Error("Pool configuration mode is unavailable.");
        if (!capability.enabled) return { enabled: false, configurations: [] };
        const first = (await client.miningGroupServiceListMiningGroups(options)).data.groups ?? [];
        if (new Set(first.map((group) => group.id)).size !== first.length)
          throw new Error("Duplicate pool configuration identity.");
        const memberships =
          (await client.miningGroupServiceListMiningGroupMemberships({}, options)).data.memberships ?? [];
        const last = (await client.miningGroupServiceListMiningGroups(options)).data.groups ?? [];
        const after = (await client.miningGroupServiceGetMiningGroupCapability(options)).data.capability;
        if (
          capability.stateRevision !== after?.stateRevision ||
          !after?.enabled ||
          JSON.stringify(groupStamp(first)) !== JSON.stringify(groupStamp(last))
        )
          throw new Error("Pool configurations changed. Refresh statistics.");
        const seen = new Set<string>();
        const counts = new Map<string, number>();
        for (const membership of memberships) {
          const mac = membership.macAddress?.toLowerCase();
          if (!mac || !membership.groupId || seen.has(mac) || !first.some((group) => group.id === membership.groupId))
            throw new Error("Pool configuration assignments could not be confirmed.");
          seen.add(mac);
          counts.set(membership.groupId, (counts.get(membership.groupId) ?? 0) + 1);
        }
        return {
          enabled: true,
          configurations: first
            .filter((group) => counts.has(group.id ?? ""))
            .map((group) => {
              if (!group.id || !/^[1-9]\d*$/.test(group.revision ?? ""))
                throw new Error("Pool configuration identity is unavailable.");
              return {
                id: group.id,
                name: group.name || "Pool configuration",
                revision: group.revision!,
                assignedCount: counts.get(group.id)!,
              };
            })
            .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true })),
        };
      }),
    read: (configurations, params) =>
      read(params, async (options) => {
        const result = await mapWithConcurrency(
          configurations,
          4,
          async (configuration) => {
            const settings =
              configuration.id === "default"
                ? await readDefaultPools(client, options)
                : ((await client.miningGroupServiceListMiningGroupPools({ groupId: configuration.id }, options)).data
                    .pools ?? []);
            if (
              settings.some(
                (pool) => !/^[1-9]\d*$/.test(String(pool.id ?? "")) || !Number.isSafeInteger(Number(pool.id)),
              )
            )
              throw new Error("Pool identity is unavailable.");
            let diagnostics: Awaited<ReturnType<MdkV2Client["poolServiceGetPoolDiagnostics"]>>["data"];
            let observedBoards: number | undefined;
            let error: string | undefined;
            try {
              if (configuration.id === "default")
                diagnostics = (await client.poolServiceGetPoolDiagnostics({ includeLastRecordedShares: true }, options))
                  .data;
              else {
                const response = (
                  await client.miningGroupServiceGetMiningGroupPoolDiagnostics({ groupId: configuration.id }, options)
                ).data;
                diagnostics = { pools: response.pools };
                observedBoards = new Set((response.observedMacAddresses ?? []).map((mac) => mac.toLowerCase())).size;
                if (observedBoards > configuration.assignedCount)
                  throw new Error("Board coverage could not be confirmed.");
              }
            } catch (cause) {
              if (options.signal?.aborted || isAuthenticationError(cause)) throw cause;
              diagnostics = {};
              observedBoards = undefined;
              error = "Connection status is unavailable.";
            }
            const current = (await client.miningGroupServiceGetMiningGroup({ id: configuration.id }, options)).data
              .group;
            if (current?.revision !== configuration.revision)
              throw new Error("Pool configurations changed. Refresh statistics.");
            const shares = toCanonicalPoolRecordedShares(diagnostics);
            const pools = toCanonicalPools(
              { pools: settings.map((pool) => ({ ...pool, id: String(pool.id) })) },
              diagnostics,
            ).map((pool) => ({
              ...pool,
              difficulty: pool.status === "Unknown" ? undefined : pool.difficulty,
              ...(shares[pool.id]
                ? {
                    lastShareAtMs: shares[pool.id]!.resultAtMs,
                    lastAssignedDifficulty: shares[pool.id]!.assignedDifficulty,
                    lastShareOutcome: shares[pool.id]!.outcome,
                  }
                : {}),
            }));
            return { ...configuration, pools, error, observedBoards };
          },
          options.signal ?? undefined,
        );
        const ids = result.flatMap((configuration) => configuration.pools.map((pool) => pool.id));
        if (new Set(ids).size !== ids.length)
          throw new Error("The controller reported duplicate pool IDs. Statistics cannot be separated safely.");
        return result;
      }),
  };
}
