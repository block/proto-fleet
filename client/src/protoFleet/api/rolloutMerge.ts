import type {
  RolloutBatchEvidenceSummary,
  RolloutGroup,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";

function newestEvidenceSummary(
  existing: RolloutBatchEvidenceSummary | undefined,
  incoming: RolloutBatchEvidenceSummary | undefined,
): RolloutBatchEvidenceSummary | undefined {
  if (!existing || !incoming) {
    return existing ?? incoming;
  }

  const existingFinalized =
    existing.postWindowFinalized || existing.status === "finalized" || existing.status === "cancelled";
  const incomingFinalized =
    incoming.postWindowFinalized || incoming.status === "finalized" || incoming.status === "cancelled";
  if (existingFinalized !== incomingFinalized) {
    return existingFinalized ? existing : incoming;
  }

  const existingEvaluatedAt = existing.evaluatedAt ? Date.parse(existing.evaluatedAt) : Number.NaN;
  const incomingEvaluatedAt = incoming.evaluatedAt ? Date.parse(incoming.evaluatedAt) : Number.NaN;
  if (Number.isFinite(existingEvaluatedAt) && !Number.isFinite(incomingEvaluatedAt)) {
    return existing;
  }
  if (Number.isFinite(incomingEvaluatedAt) && !Number.isFinite(existingEvaluatedAt)) {
    return incoming;
  }
  if (Number.isFinite(existingEvaluatedAt) && Number.isFinite(incomingEvaluatedAt)) {
    return existingEvaluatedAt > incomingEvaluatedAt ? existing : incoming;
  }
  return incoming;
}

export function newestRollout(existing: RolloutRecord | undefined, incoming: RolloutRecord): RolloutRecord {
  if (!existing || existing.revision > incoming.revision) {
    return existing ?? incoming;
  }

  const existingBatchById = new Map(existing.batches.map((batch) => [batch.id, batch]));
  const preserveDetailedMembers = incoming.summaryOnly && !existing.summaryOnly;
  return {
    ...incoming,
    summaryOnly: preserveDetailedMembers ? false : incoming.summaryOnly,
    members: preserveDetailedMembers ? existing.members : incoming.members,
    causes: preserveDetailedMembers ? existing.causes : incoming.causes,
    batches: incoming.batches.map((batch) => {
      const existingBatch = existingBatchById.get(batch.id);
      return {
        ...batch,
        members: preserveDetailedMembers ? (existingBatch?.members ?? batch.members) : batch.members,
        evidenceSummary: newestEvidenceSummary(existingBatch?.evidenceSummary, batch.evidenceSummary),
      };
    }),
  };
}

export function newestRolloutGroup(existing: RolloutGroup | undefined, incoming: RolloutGroup): RolloutGroup {
  if (!existing) {
    return incoming;
  }
  const existingChildren = new Map(existing.children.map((child) => [child.id, child]));
  return {
    ...incoming,
    children: incoming.children.map((child) => newestRollout(existingChildren.get(child.id), child)),
  };
}
