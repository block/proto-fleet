import { useIsAwake } from "@/protoOS/store";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

interface PowerPopoverProps {
  onReboot: () => void;
  onSleep: () => void;
  onWake: () => void;
}

const PowerPopover = ({ onReboot, onSleep, onWake }: PowerPopoverProps) => {
  const isAwake = useIsAwake();

  return (
    <Popover
      title="Power"
      subtitle={isAwake ? "Reboot or put your miner into sleep mode." : "Reboot or wake up your miner."}
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
