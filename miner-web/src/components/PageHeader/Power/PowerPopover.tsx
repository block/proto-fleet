import { useMemo } from "react";

import { MiningStatusMiningstatus } from "apiTypes";

import { positions } from "common/constants";

import { variants } from "components/Button";
import { groupVariants } from "components/ButtonGroup";
import Popover, { popoverSizes } from "components/Popover";

interface PowerPopoverProps {
  miningStatus: MiningStatusMiningstatus;
  onReboot: () => void;
  onSleep: () => void;
  onWake: () => void;
}

const PowerPopover = ({
  miningStatus,
  onReboot,
  onSleep,
  onWake,
}: PowerPopoverProps) => {
  const isMining = useMemo(
    () => !Object.keys(miningStatus).length || miningStatus?.status === "Running",
    [miningStatus]
  );

  return (
    <Popover
      title="Power"
      subtitle={
        isMining
          ? "Reboot or put your miner into sleep mode."
          : "Reboot or wake your miner."
      }
      size={popoverSizes.small}
      buttons={[
        {
          text: "Reboot",
          onClick: onReboot,
          variant: variants.secondary,
        },
        {
          text: isMining ? "Sleep" : "Wake",
          onClick: isMining ? onSleep : onWake,
          variant: variants.secondary,
        },
      ]}
      buttonGroupVariant={groupVariants.stack}
      position={positions["bottom left"]}
      testId="power-popover"
    />
  );
};

export default PowerPopover;
