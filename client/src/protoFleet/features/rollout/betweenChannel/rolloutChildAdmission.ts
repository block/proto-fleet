import type { AdmitRolloutInput } from "@/protoFleet/api/useRolloutApi";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

interface ChildAdmissionOptions {
  rollout: RolloutRecord;
  batchId: bigint;
  admissionAttempt: number;
  reason: string;
  admit: (input: AdmitRolloutInput) => Promise<RolloutRecord>;
  updateState: (rolloutId: string, state: { loading: boolean; error?: string }) => void;
  onAdmitted: (rollout: RolloutRecord) => void;
}

export async function admitRolloutChild({
  rollout,
  batchId,
  admissionAttempt,
  reason,
  admit,
  updateState,
  onAdmitted,
}: ChildAdmissionOptions): Promise<RolloutRecord> {
  updateState(rollout.id, { loading: true });
  try {
    const admitted = await admit({
      rolloutId: rollout.id,
      batchId,
      expectedRevision: rollout.revision,
      idempotencyKey: `${rollout.id}:${batchId}:admit:${admissionAttempt}`,
      reason,
    });
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
