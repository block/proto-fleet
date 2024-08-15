import { useMemo } from "react";

import { MiningStatusMiningstatus } from "apiTypes";

import { positions } from "common/constants";

import { isSleeping } from "components/App/utility";
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
  const isAwake = useMemo(
    () => !isSleeping(miningStatus?.status),
    [miningStatus]
  );

  return (
    <Popover
      title="Power"
      subtitle={
        isAwake
          ? "Reboot or put your miner into sleep mode."
          : "Reboot or wake up your miner."
      }
      size={popoverSizes.small}
      buttons={[
        {
          text: "Reboot",
          onClick: onReboot,
          variant: variants.secondary,
          testId: "popover-reboot-button",
        },
        {
          text: isAwake ? "Sleep" : "Wake up",
          onClick: isAwake ? onSleep : onWake,
          variant: variants.secondary,
          testId: isAwake ? "popover-sleep-button" : "popover-wake-up-button",
        },
      ]}
      buttonGroupVariant={groupVariants.stack}
      position={positions["bottom left"]}
      testId="power-popover"
    />
  );
};

export default PowerPopover;
