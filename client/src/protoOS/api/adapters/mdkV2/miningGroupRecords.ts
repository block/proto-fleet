import type { MiningGroupPoolSettings } from "../poolConfiguration";
import type { V2MiningGroupPoolRecord, V2MiningGroupRecord, V2Pool } from "./generated/generatedApi";

export const toPoolSettings = (pool: V2MiningGroupPoolRecord | V2Pool): MiningGroupPoolSettings => ({
  id: String(pool.id ?? ""),
  name: pool.name ?? "",
  url: pool.url ?? "",
  username: pool.username ?? "",
  priority: pool.priority ?? 0,
  enabled: pool.enabled === true,
  shareWeight: pool.shareWeight,
});

export const groupStamp = (groups: V2MiningGroupRecord[]) =>
  groups.map(({ id, revision }) => [id, revision]).sort(([left], [right]) => (left ?? "").localeCompare(right ?? ""));
