import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";

export const getActivityBatchId = (entry: Pick<ActivityEntry, "batchId" | "metadata">): string | undefined => {
  if (entry.batchId) {
    return entry.batchId;
  }

  const metadataBatchId = entry.metadata?.batch_id;
  return typeof metadataBatchId === "string" && metadataBatchId.length > 0 ? metadataBatchId : undefined;
};
