import { useMemo } from "react";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { isFirmwareConvergenceReady } from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import FirmwareTransitionMinerDetails from "@/protoFleet/features/rollout/FirmwareTransitionMinerDetails";
import { mapFirmwareTransitionToRolloutEvent } from "@/protoFleet/features/rollout/firmwareTransitionRolloutEvent";
import type { RolloutLane } from "@/protoFleet/features/rollout/rolloutTypes";
import { Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";

interface LaneFirmwareConvergenceStatusProps {
  lane: RolloutLane;
  canStart: boolean;
  onClose?: () => void;
  onStart: () => void;
}

export default function LaneFirmwareConvergenceStatus({
  lane,
  canStart,
  onClose,
  onStart,
}: LaneFirmwareConvergenceStatusProps) {
  const ready = isFirmwareConvergenceReady(lane);
  const event = useMemo(
    () =>
      mapFirmwareTransitionToRolloutEvent(lane.firmwareConvergence, {
        scopeLabel: lane.label,
        startedAt: lane.createdAt,
      }),
    [lane.createdAt, lane.firmwareConvergence, lane.label],
  );

  return (
    <section className="grid gap-5" aria-labelledby="lane-firmware-convergence-title">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <div>
          <div id="lane-firmware-convergence-title" className="text-heading-100 text-text-primary">
            Firmware convergence
          </div>
          <div className="mt-1 text-300 text-text-primary-70">{lane.label}</div>
        </div>
        {onClose ? (
          <Button
            text="Close status"
            variant={variants.secondary}
            size={sizes.compact}
            className="phone:w-full"
            onClick={onClose}
          />
        ) : null}
      </div>

      {ready ? (
        <Callout
          intent={intents.success}
          prefixIcon={<Success />}
          title="Lane ready"
          subtitle="Every current member is confirmed on the lane firmware."
          buttonText={canStart ? "Start rollout" : undefined}
          buttonOnClick={canStart ? onStart : undefined}
        />
      ) : null}

      <ActiveRolloutStatus event={event} hideActions />
      <FirmwareTransitionMinerDetails key={lane.id} progress={lane.firmwareConvergence} />
    </section>
  );
}
