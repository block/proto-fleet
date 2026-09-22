import type { Rollout } from "./generated/rollout/v1/rollout_pb";

// Merge independent reads without regressing revisions; later sources win ties
// because live telemetry can change without advancing a rollout revision.
export function mergeRollouts(...sources: readonly (readonly Rollout[])[]): Rollout[] {
  const byId = new Map<bigint, Rollout>();
  for (const source of sources) {
    for (const rollout of source) {
      const current = byId.get(rollout.id);
      if (!current || rollout.revision >= current.revision) byId.set(rollout.id, rollout);
    }
  }
  return [...byId.values()].sort((a, b) => {
    const seconds = (b.createdAt?.seconds ?? 0n) - (a.createdAt?.seconds ?? 0n);
    if (seconds !== 0n) return seconds > 0n ? 1 : -1;
    const nanos = (b.createdAt?.nanos ?? 0) - (a.createdAt?.nanos ?? 0);
    if (nanos !== 0) return nanos;
    return a.id === b.id ? 0 : a.id < b.id ? 1 : -1;
  });
}
