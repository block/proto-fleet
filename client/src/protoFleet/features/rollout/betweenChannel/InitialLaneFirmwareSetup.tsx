import { useMemo } from "react";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { isInitialFirmwareReady } from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import FirmwareTransitionMinerDetails from "@/protoFleet/features/rollout/FirmwareTransitionMinerDetails";
import { mapFirmwareTransitionToRolloutEvent } from "@/protoFleet/features/rollout/firmwareTransitionRolloutEvent";
import type { RolloutLane } from "@/protoFleet/features/rollout/rolloutTypes";
import { Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";

interface InitialLaneFirmwareSetupProps {
  lane: RolloutLane;
  canStart: boolean;
  onClose?: () => void;
  onStart: () => void;
}

export default function InitialLaneFirmwareSetup({ lane, canStart, onClose, onStart }: InitialLaneFirmwareSetupProps) {
  const ready = isInitialFirmwareReady(lane);
  const event = useMemo(
    () =>
      mapFirmwareTransitionToRolloutEvent(lane.initialEnforcement, {
        scopeLabel: lane.label,
        startedAt: lane.createdAt,
      }),
    [lane.createdAt, lane.initialEnforcement, lane.label],
  );

  return (
    <section className="grid gap-5" aria-labelledby="initial-lane-firmware-setup-title">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <div>
          <div id="initial-lane-firmware-setup-title" className="text-heading-100 text-text-primary">
            Initial firmware setup
          </div>
          <div className="mt-1 text-300 text-text-primary-70">{lane.label}</div>
        </div>
        {onClose ? (
          <Button
            text="Close setup"
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
          subtitle="Every initial member is confirmed on the target firmware."
          buttonText={canStart ? "Start rollout" : undefined}
          buttonOnClick={canStart ? onStart : undefined}
        />
      ) : null}

      <ActiveRolloutStatus event={event} hideActions />
      <FirmwareTransitionMinerDetails key={lane.id} progress={lane.initialEnforcement} />
    </section>
  );
}
