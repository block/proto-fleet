import { useMemo, useState } from "react";

import { mapRolloutToEvent } from "@/protoFleet/api/rolloutMappers";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  canCompleteWithFailures,
  canRevertRollout,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import type {
  RolloutEventEvidence,
  RolloutEvidenceStatus,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";
import { useHeldRolloutOverride } from "@/protoFleet/features/rollout/useHeldRolloutOverride";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface BetweenChannelRolloutStatusProps {
  rollout: RolloutRecord;
  laneLabel: string;
  canControl: boolean;
  isMutating?: boolean;
  onPause?: () => void;
  onResume?: () => void;
  onContinue?: (reason?: string) => void;
  onAbort?: () => void;
  onRevert?: () => void;
  onCompleteWithFailures?: () => void;
}

const EVIDENCE_STALE_AFTER_MS = 20_000;

const evidenceStatusContent: Record<RolloutEvidenceStatus, { label: string; detail: string }> = {
  pending: {
    label: "Evidence pending",
    detail: "Waiting for this batch to complete before evidence collection starts.",
  },
  collecting: {
    label: "Collecting evidence",
    detail: "Collecting paired post-update hashrate for this batch.",
  },
  unavailable: {
    label: "Evidence unavailable",
    detail: "Paired evidence is incomplete and is not treated as zero.",
  },
  observing: {
    label: "Observing hashrate",
    detail: "Paired hashrate is available while the health window is observed.",
  },
  healthy: {
    label: "Hashrate healthy",
    detail: "The latest policy health checks are within the configured threshold.",
  },
  held: {
    label: "Hashrate held",
    detail: "The latest policy health check exceeded the configured drop threshold.",
  },
  stale: {
    label: "Evidence stale",
    detail: "Telemetry samples or evaluator updates are older than 20 seconds. Manual controls remain available.",
  },
  automationError: {
    label: "Automation error",
    detail: "Automatic continue failed and will not be retried. Manual controls remain available.",
  },
  finalized: {
    label: "Evidence finalized",
    detail: "The completed post-update window is finalized.",
  },
  unknown: {
    label: "Evidence status unavailable",
    detail: "The server did not provide a recognized evidence status.",
  },
};

function displayedEvidenceStatus(evidence: RolloutEventEvidence): RolloutEvidenceStatus {
  if (
    evidence.status === "collecting" ||
    evidence.status === "observing" ||
    evidence.status === "healthy" ||
    evidence.status === "held"
  ) {
    const evaluatedAtMs = evidence.evaluatedAt ? Date.parse(evidence.evaluatedAt) : Number.NaN;
    if (Number.isFinite(evaluatedAtMs) && Date.now() - evaluatedAtMs > EVIDENCE_STALE_AFTER_MS) {
      return "stale";
    }
  }
  return evidence.status;
}

function formatBasisPointDelta(basisPoints: number): string {
  const percent = basisPoints / 100;
  const sign = percent > 0 ? "+" : percent < 0 ? "−" : "";
  return `${sign}${Math.abs(percent).toFixed(2)}%`;
}

function formatEvidenceTime(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(new Date(value));
}

function RolloutEvidenceStatusCard({ evidence }: { evidence: RolloutEventEvidence }) {
  const status = displayedEvidenceStatus(evidence);
  const content = evidenceStatusContent[status];
  const detail = status === "automationError" && evidence.errorMessage ? evidence.errorMessage : content.detail;
  const hasPolicyBucket = evidence.policy && evidence.latestPolicyBucketDeltaBasisPoints !== undefined;

  return (
    <div
      className="grid gap-4 rounded-xl bg-surface-elevated-base p-5 text-text-primary shadow-100 tablet:grid-cols-3"
      data-testid="rollout-evidence-status"
    >
      <div className="min-w-0">
        <div className="text-200 text-text-primary-50">{evidence.batchLabel} evidence</div>
        <div className="mt-1 text-emphasis-300 text-text-primary" role="status" aria-live="polite" aria-atomic="true">
          {content.label}
        </div>
        <div className="mt-1 text-200 text-text-primary-70">{detail}</div>
      </div>
      <div className="min-w-0">
        <div className="text-200 text-text-primary-50">Coverage</div>
        <div className="mt-1 text-emphasis-300 text-text-primary">
          Paired coverage {evidence.pairedCount.toLocaleString()} of {evidence.totalCount.toLocaleString()}
        </div>
        <div className="mt-1 text-200 text-text-primary-70">
          Post window {evidence.postWindowFinalized ? "finalized" : "open"}
        </div>
        {evidence.evaluatedAt ? (
          <div className="mt-1 text-200 text-text-primary-70">
            Last evaluated {formatEvidenceTime(evidence.evaluatedAt)}
          </div>
        ) : null}
        {evidence.postWindowFinalizedAt ? (
          <div className="mt-1 text-200 text-text-primary-70">
            Finalized {formatEvidenceTime(evidence.postWindowFinalizedAt)}
          </div>
        ) : null}
      </div>
      {hasPolicyBucket ? (
        <div className="min-w-0">
          <div className="text-200 text-text-primary-50">Latest policy health check</div>
          <div className="mt-1 text-emphasis-300 text-text-primary">
            {formatBasisPointDelta(evidence.latestPolicyBucketDeltaBasisPoints!)}
          </div>
          <div className="mt-1 text-200 text-text-primary-70">
            Cumulative performance appears in the comparison below.
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function BetweenChannelRolloutStatus({
  rollout,
  laneLabel,
  canControl,
  isMutating = false,
  onPause,
  onResume,
  onContinue,
  onAbort,
  onRevert,
  onCompleteWithFailures,
}: BetweenChannelRolloutStatusProps) {
  const [confirmation, setConfirmation] = useState<"abort" | "revert" | null>(null);
  const event = useMemo(() => mapRolloutToEvent(rollout, { laneLabel }), [laneLabel, rollout]);
  const heldOverride = useHeldRolloutOverride(event.evidence, onContinue);
  const confirmedCount = rollout.members.filter((member) => member.state === "succeeded").length;
  const sourceRemaining = Math.max(rollout.members.length - confirmedCount, 0);

  return (
    <>
      <div className="grid gap-4">
        <ActiveRolloutStatus
          event={event}
          canManage={false}
          canControl={Boolean(canControl && !isMutating)}
          onPause={onPause}
          onResume={onResume}
          onContinueFromReview={heldOverride.onContinue}
          onAbort={onAbort ? () => setConfirmation("abort") : undefined}
          onRevert={onRevert && canRevertRollout(rollout) ? () => setConfirmation("revert") : undefined}
          onCompleteWithFailures={canCompleteWithFailures(rollout) ? onCompleteWithFailures : undefined}
        />
        {event.evidence ? <RolloutEvidenceStatusCard evidence={event.evidence} /> : null}
        <div className="grid gap-2 rounded-xl bg-surface-elevated-base p-5 text-300 text-text-primary-70 shadow-100 tablet:grid-cols-2">
          <div>
            <div className="text-200 text-text-primary-50">Source release</div>
            <div className="mt-1 text-emphasis-300 text-text-primary">
              {sourceRemaining.toLocaleString()} miners remain on the current release
            </div>
          </div>
          <div>
            <div className="text-200 text-text-primary-50">Target membership</div>
            <div className="mt-1 text-emphasis-300 text-text-primary">
              {confirmedCount.toLocaleString()} miners confirmed and moved
            </div>
          </div>
        </div>
      </div>

      {confirmation === "abort" ? (
        <Dialog
          open
          title="Abort rollout?"
          icon={<Alert className="text-intent-critical-fill" />}
          subtitle="Undispatched miners remain on the current release. In-flight work may still settle after the abort boundary. This does not revert miners that already moved."
          onDismiss={() => setConfirmation(null)}
          buttons={[
            {
              text: "Abort rollout",
              testId: "confirm-abort-rollout",
              variant: variants.danger,
              onClick: () => {
                setConfirmation(null);
                onAbort?.();
              },
            },
            {
              text: "Cancel",
              variant: variants.secondary,
              onClick: () => setConfirmation(null),
            },
          ]}
        />
      ) : null}

      {confirmation === "revert" ? (
        <Dialog
          open
          title={`Revert ${confirmedCount.toLocaleString()} confirmed miner${confirmedCount === 1 ? "" : "s"}?`}
          icon={<Alert className="text-intent-critical-fill" />}
          subtitle="Revert restores the captured source firmware first, then moves only confirmed eligible miners back to the source release."
          onDismiss={() => setConfirmation(null)}
          buttons={[
            {
              text: "Revert firmware",
              testId: "confirm-revert-rollout",
              variant: variants.danger,
              onClick: () => {
                setConfirmation(null);
                onRevert?.();
              },
            },
            {
              text: "Cancel",
              variant: variants.secondary,
              onClick: () => setConfirmation(null),
            },
          ]}
        />
      ) : null}
      {heldOverride.confirmationDialog}
    </>
  );
}
