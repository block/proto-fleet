import type { AssignmentPool } from "../poolAssignment";
import type { MiningGroupPoolSettings } from "../poolConfiguration";
import type { AdapterRequestOptions } from "../types";
import type { MdkV2Client } from "./connectClient";
import { withDeadline } from "./controllerRecords";
import { toPoolSettings } from "./miningGroupRecords";

export const publicPools = (pools: MiningGroupPoolSettings[]) =>
  pools
    .map(({ id, name, url, username, priority, enabled, shareWeight }) => ({
      id,
      name,
      url,
      username,
      priority,
      enabled,
      shareWeight,
    }))
    .sort((a, b) => a.priority - b.priority);

export async function readDefaultPools(client: MdkV2Client, options: AdapterRequestOptions) {
  const { data } = await withDeadline(options, 10_000, (params) => client.poolServiceListPools(params));
  return publicPools(
    (data.pools ?? []).map((pool) => {
      if (!/^[1-9]\d*$/.test(pool.id ?? "") || !Number.isSafeInteger(Number(pool.id)))
        throw new Error("The controller did not report a valid Default pool identity.");
      return toPoolSettings(pool);
    }),
  );
}

export function defaultPoolSetError(
  pools: Array<Pick<MiningGroupPoolSettings, "enabled" | "url" | "username" | "shareWeight">>,
) {
  const enabled = pools.filter((pool) => pool.enabled);
  const weighted = enabled.some((pool) => pool.shareWeight !== undefined);
  const limit = weighted ? 48 : 3;
  if (enabled.length > limit) return `Default supports at most ${limit} enabled pools in this allocation mode.`;
  const identities = enabled.map((pool) =>
    JSON.stringify([pool.url.trim().toLowerCase(), weighted ? pool.username : null]),
  );
  if (new Set(identities).size !== enabled.length)
    return weighted
      ? "Weighted Default pools must use distinct URL and username pairs."
      : "Default priority pools must use different URLs.";
}

type PoolWrite =
  | { action: "remove"; id: string }
  | { action: "add"; pool: MiningGroupPoolSettings; password: string }
  | { action: "update"; pool: MiningGroupPoolSettings; fields: string; password?: string };

const stamp = (value: MiningGroupPoolSettings[]) => JSON.stringify(publicPools(value));
const endpoint = (value: string) => {
  const url = value.trim();
  if (!url.includes("://")) return `stratum+tcp://${url}`;
  return url.replace(/^tcp:\/\//i, "stratum+tcp://").replace(/^ssl:\/\//i, "stratum+ssl://");
};
const confirmPool = (
  receipt: Parameters<typeof toPoolSettings>[0] | undefined,
  desired: MiningGroupPoolSettings,
  normalizeURL: boolean,
  normalizeName = false,
) => {
  const actual = receipt && toPoolSettings(receipt);
  if (
    !actual ||
    stamp([
      {
        ...actual,
        url: normalizeURL && endpoint(actual.url) === endpoint(desired.url) ? desired.url : actual.url,
        name: normalizeName && actual.name === desired.name.trim() ? desired.name : actual.name,
      },
    ]) !== stamp([desired])
  ) {
    throw new Error("The controller returned unexpected Default pool settings. Reload and review before trying again.");
  }
  return actual;
};
const validSequence = (pools: MiningGroupPoolSettings[]) => {
  const priorities = pools.filter((pool) => pool.enabled).map((pool) => pool.priority);
  return new Set(priorities).size === priorities.length && !defaultPoolSetError(pools);
};

function planDefaultWrites(original: MiningGroupPoolSettings[], inputs: AssignmentPool[]) {
  const desired = inputs.map((pool, priority) => ({
    id: pool.id ?? `new:${priority}`,
    name: pool.name,
    url: pool.url,
    username: pool.username,
    priority,
    enabled: pool.enabled ?? true,
    shareWeight: pool.shareWeight,
  }));
  const problem = defaultPoolSetError(desired);
  if (problem) throw new Error(problem);

  let current = original;
  const writes: PoolWrite[] = [];
  const replace = (pool: MiningGroupPoolSettings) => current.map((entry) => (entry.id === pool.id ? pool : entry));
  const stage = (pool: MiningGroupPoolSettings, fields: string, password?: string) => {
    writes.push({ action: "update", pool, fields, password });
    current = replace(pool);
  };
  let temporary = Math.max(desired.length, ...original.map((pool) => pool.priority + 1));
  for (const pool of original) {
    const next = desired.find((entry) => entry.id === pool.id);
    if (next && pool.priority !== next.priority) {
      if (temporary > 2147483647) throw new Error("Pool priorities exceed the supported range.");
      stage({ ...pool, priority: temporary++ }, "priority");
    }
  }

  const remaining = new Set(desired);
  while (remaining.size || current.some((entry) => !desired.some((pool) => entry.id === pool.id))) {
    let advanced = false;
    for (const pool of remaining) {
      const input = inputs[pool.priority];
      const existing = current.find((entry) => entry.id === pool.id);
      const password = input.preservePassword ? undefined : input.password;
      if (existing) {
        if (!validSequence(replace(pool))) continue;
        const fields = (["url", "username", "priority", "enabled"] as const).filter(
          (field) => pool[field] !== existing[field],
        );
        if (fields.length || password !== undefined) stage(pool, fields.join(",") || "id", password);
      } else {
        const next = [...current, pool];
        if (current.some((entry) => entry.name === pool.name) || !validSequence(next)) continue;
        writes.push({ action: "add", pool, password: input.password });
        current = next;
      }
      remaining.delete(pool);
      advanced = true;
    }
    for (const pool of current) {
      if (desired.some((entry) => entry.id === pool.id)) continue;
      const next = current.filter((entry) => entry.id !== pool.id);
      if (!validSequence(next) || (remaining.size && pool.enabled && !next.some((entry) => entry.enabled))) continue;
      writes.push({ action: "remove", id: pool.id });
      current = next;
      advanced = true;
    }
    if (advanced) continue;

    // Break an endpoint swap only when a different pool can stay enabled.
    const park = current.find(
      (pool) =>
        pool.enabled &&
        [...remaining].some(
          (entry) => entry.id === pool.id && (entry.url !== pool.url || entry.username !== pool.username),
        ) &&
        current.some((entry) => entry.enabled && entry.id !== pool.id) &&
        validSequence(replace({ ...pool, enabled: false })),
    );
    if (!park) throw new Error("This Default change needs an intermediate pool configuration. No changes were saved.");
    stage({ ...park, enabled: false }, "enabled");
  }
  return { desired, writes };
}

/** The legacy API has no atomic replace or revision argument. Check before every write; never retry it. */
export async function saveDefaultPools(
  client: MdkV2Client,
  original: MiningGroupPoolSettings[],
  pools: AssignmentPool[],
  options: AdapterRequestOptions,
  checkAssignments: () => Promise<void>,
  progress: (phase: string, pools: MiningGroupPoolSettings[]) => void,
) {
  const { desired, writes } = planDefaultWrites(original, pools);
  const allocated = new Map<string, string>();
  const resolve = (pool: MiningGroupPoolSettings) => ({ ...pool, id: allocated.get(pool.id) ?? pool.id });
  const wanted = () => desired.map(resolve);
  let expected: MiningGroupPoolSettings[] = publicPools(original);
  const checkPools = async () => {
    if (stamp(await readDefaultPools(client, options)) !== stamp(expected))
      throw new Error("Default pools changed. Some changes may be saved. Reload and review before trying again.");
  };
  for (const write of writes) {
    await checkAssignments();
    await checkPools();
    if (write.action === "remove") {
      progress("Removing a Default pool", wanted());
      await withDeadline(options, 10_000, (params) => client.poolServiceDeletePool({ id: write.id }, params));
      expected = expected.filter((pool) => pool.id !== write.id);
    } else if (write.action === "add") {
      progress("Adding a Default pool", wanted());
      const { data } = await withDeadline(options, 10_000, (params) =>
        client.poolServiceCreatePool({ pool: { ...write.pool, id: undefined }, password: write.password }, params),
      );
      if (!data.pool?.id || !/^[1-9]\d*$/.test(data.pool.id))
        throw new Error("The new Default pool identity is not confirmed. Reload pools before trying again.");
      allocated.set(write.pool.id, data.pool.id);
      expected.push(confirmPool(data.pool, resolve(write.pool), true, true));
    } else {
      progress("Saving Default pool settings", wanted());
      const pool = resolve(write.pool);
      const { data } = await withDeadline(options, 10_000, (params) =>
        client.poolServiceUpdatePool(
          { poolId: pool.id },
          { pool, password: write.password, updateMask: write.fields },
          params,
        ),
      );
      const fields = write.fields.split(",");
      const receiptBase = expected.find((entry) => entry.id === pool.id)!;
      const desired = {
        ...receiptBase,
        url: fields.includes("url") ? pool.url : receiptBase.url,
        username: fields.includes("username") ? pool.username : receiptBase.username,
        priority: fields.includes("priority") ? pool.priority : receiptBase.priority,
        enabled: fields.includes("enabled") ? pool.enabled : receiptBase.enabled,
      };
      const confirmed = confirmPool(data.pool, desired, fields.includes("url"));
      expected = expected.map((entry) => (entry.id === pool.id ? confirmed : entry));
    }
  }
  await checkAssignments();
  await checkPools();
  return publicPools(expected);
}
