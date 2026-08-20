import type { RolloutBatch, RolloutRecord } from "./rolloutTypes";

export function latestCompletedRolloutBatch(rollout: Pick<RolloutRecord, "batches">): RolloutBatch | undefined {
  let latest: RolloutBatch | undefined;
  for (const batch of rollout.batches) {
    if (
      batch.state === "completed" &&
      (!latest || batch.position > latest.position || (batch.position === latest.position && batch.id > latest.id))
    ) {
      latest = batch;
    }
  }
  return latest;
}
