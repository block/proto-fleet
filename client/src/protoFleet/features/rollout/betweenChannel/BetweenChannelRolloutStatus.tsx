import { useMemo, useState } from "react";

import { mapRolloutToEvent } from "@/protoFleet/api/rolloutMappers";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  canCompleteWithFailures,
  canRevertRollout,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
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
  onContinue?: () => void;
  onAbort?: () => void;
  onRevert?: () => void;
  onCompleteWithFailures?: () => void;
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
          onContinueFromReview={onContinue}
          onAbort={onAbort ? () => setConfirmation("abort") : undefined}
          onRevert={onRevert && canRevertRollout(rollout) ? () => setConfirmation("revert") : undefined}
          onCompleteWithFailures={canCompleteWithFailures(rollout) ? onCompleteWithFailures : undefined}
        />
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
    </>
  );
}
