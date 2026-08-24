import type { AdmitRolloutInput } from "@/protoFleet/api/useRolloutApi";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

interface ChildAdmissionOptions {
  rollout: RolloutRecord;
  batchId: bigint;
  admissionAttempt: number;
  keyPrefix?: string;
  reason: string;
  admit: (input: AdmitRolloutInput) => Promise<RolloutRecord>;
  updateState: (rolloutId: string, state: { loading: boolean; error?: string }) => void;
  onAdmitted: (rollout: RolloutRecord) => void;
}

export function admissionKeyPrefixStorageKey(rolloutId: string): string {
  return `protoFleet.rolloutAdmissionKeyPrefix.${rolloutId}`;
}

export async function admitRolloutChild({
  rollout,
  batchId,
  admissionAttempt,
  keyPrefix,
  reason,
  admit,
  updateState,
  onAdmitted,
}: ChildAdmissionOptions): Promise<RolloutRecord> {
  const storageKey = admissionKeyPrefixStorageKey(rollout.id);
  const stablePrefix = keyPrefix ?? localStorage.getItem(storageKey) ?? rollout.id;
  if (keyPrefix) {
    localStorage.setItem(storageKey, keyPrefix);
  }
  updateState(rollout.id, { loading: true });
  try {
    const admitted = await admit({
      rolloutId: rollout.id,
      batchId,
      expectedRevision: rollout.revision,
      idempotencyKey: `${stablePrefix}:admit:${admissionAttempt}`,
      reason,
    });
    localStorage.removeItem(storageKey);
    updateState(rollout.id, { loading: false });
    onAdmitted(admitted);
    return admitted;
  } catch (error) {
    const modelLabel = [rollout.manufacturer, rollout.model].filter(Boolean).join(" ") || "Model";
    updateState(rollout.id, {
      loading: false,
      error:
        error instanceof Error ? `${modelLabel} couldn't start: ${error.message}` : `${modelLabel} couldn't start.`,
    });
    throw error;
  }
}
