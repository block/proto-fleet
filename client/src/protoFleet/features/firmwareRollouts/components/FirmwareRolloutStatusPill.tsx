import type { ReactElement } from "react";
import clsx from "clsx";

import { type FirmwareRolloutState } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { firmwareRolloutStateConfig } from "@/protoFleet/features/firmwareRollouts/firmwareRolloutDisplayUtils";

interface FirmwareRolloutStatusPillProps {
  state: FirmwareRolloutState;
  className?: string;
}

const FirmwareRolloutStatusPill = ({ state, className }: FirmwareRolloutStatusPillProps): ReactElement => {
  const config = firmwareRolloutStateConfig(state);
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-2 rounded-full bg-surface-5 px-3 py-1 text-200 text-text-primary",
        className,
      )}
    >
      <span className={clsx("inline-block h-2 w-2 shrink-0 rounded-full", config.dotClassName)} />
      {config.label}
    </span>
  );
};

export default FirmwareRolloutStatusPill;
