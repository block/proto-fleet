import type { ReactElement } from "react";

import type { RolloutEventEvidence } from "@/protoFleet/features/rollout/rolloutTypes";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

function formatBasisPointDelta(basisPoints: number): string {
  const percent = basisPoints / 100;
  const sign = percent > 0 ? "+" : percent < 0 ? "−" : "";
  return `${sign}${Math.abs(percent).toFixed(2)}%`;
}

interface HeldRolloutOverrideDialogProps {
  evidence: RolloutEventEvidence;
  onCancel: () => void;
  onConfirm: () => void;
}

export default function HeldRolloutOverrideDialog({
  evidence,
  onCancel,
  onConfirm,
}: HeldRolloutOverrideDialogProps): ReactElement {
  return (
    <Dialog
      open
      title="Continue despite held hashrate evidence?"
      icon={<Alert className="text-intent-critical-fill" />}
      subtitle="Review the failed policy check before overriding this rollout hold."
      onDismiss={onCancel}
      buttons={[
        {
          text: "Continue anyway",
          variant: variants.danger,
          onClick: onConfirm,
        },
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onCancel,
        },
      ]}
    >
      <div className="grid gap-2 text-300 text-text-primary-70">
        <div>
          Configured maximum drop:{" "}
          {evidence.policy ? `${(evidence.policy.maxDropBasisPoints / 100).toFixed(2)}%` : "Unavailable"}
        </div>
        <div>
          Latest policy bucket:{" "}
          {evidence.latestPolicyBucketDeltaBasisPoints !== undefined
            ? formatBasisPointDelta(evidence.latestPolicyBucketDeltaBasisPoints)
            : "Unavailable"}
        </div>
        <div>
          Paired coverage: {evidence.pairedCount.toLocaleString()} of {evidence.totalCount.toLocaleString()}
        </div>
        <div>Continuing admits the next batch despite this held evidence.</div>
      </div>
    </Dialog>
  );
}
