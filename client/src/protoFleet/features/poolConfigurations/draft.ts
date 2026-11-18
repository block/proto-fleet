import type { Configuration, PoolInput } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import type { ConfigurationChange } from "@/shared/components/PoolConfigurations/ConfigurationChanges";
import { poolSlotLabel } from "@/shared/components/PoolConfigurations/types";

export const minerWorkerTemplate = "{miner_name}";

export { poolSlotLabel } from "@/shared/components/PoolConfigurations/types";

export type PoolDraft = Omit<PoolInput, "$typeName" | "password"> & {
  key: string;
  password: string;
  preservePassword: boolean;
};
export const blankPool = (): PoolDraft => ({
  key: crypto.randomUUID(),
  slotId: "",
  url: "",
  username: "",
  workerTemplate: minerWorkerTemplate,
  password: "",
  preservePassword: false,
});
export const configurationDraft = (original?: Configuration): PoolDraft[] =>
  original
    ? original.pools.map((pool) => ({
        key: pool.slotId,
        slotId: pool.slotId,
        url: pool.url,
        username: pool.username,
        workerTemplate: pool.workerTemplate,
        password: "",
        preservePassword: pool.hasPassword,
      }))
    : [blankPool()];
export const canPreservePassword = (pool: PoolDraft, original?: Configuration) => {
  const saved = original?.pools.find((item) => item.slotId === pool.slotId);
  return !!saved && saved.url === pool.url && saved.username === pool.username;
};
export const poolInput = ({ slotId, url, username, workerTemplate, password, preservePassword }: PoolDraft) => ({
  slotId,
  url,
  username,
  workerTemplate,
  ...(preservePassword ? {} : { password }),
});

export const configurationChanges = (
  original: Configuration,
  name: string,
  pools: PoolDraft[],
): ConfigurationChange[] => {
  const changes: ConfigurationChange[] = [];
  const add = (field: string, before: string, after: string) => {
    if (before !== after) changes.push({ field, before, after });
  };
  add("Name", original.name, name.trim());
  for (let i = 0; i < Math.max(original.pools.length, pools.length); i++) {
    const before = original.pools[i];
    const after = pools[i];
    const label = poolSlotLabel(i);
    add(`${label} URL`, before?.url ?? "", after?.url ?? "");
    add(`${label} username`, before?.username ?? "", after?.username ?? "");
    if (before && after && before.workerTemplate !== after.workerTemplate) {
      changes.push({
        field: `${label} worker naming`,
        before: before.workerTemplate || "No worker suffix",
        after:
          after.workerTemplate === minerWorkerTemplate
            ? "Use the miner's worker name"
            : after.workerTemplate || "No worker suffix",
      });
    }
    if (after && !after.preservePassword && (after.password || before?.hasPassword)) {
      changes.push({
        field: `${label} password`,
        before: before?.hasPassword ? "Saved password" : "No password",
        after: after.password ? "New password" : "No password",
      });
    }
  }
  return changes;
};
