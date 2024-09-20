import { variants } from "components/Button";
import ButtonGroup, { groupVariants, sizes } from "components/ButtonGroup";
import Callout, { intents } from "components/Callout";
import Dialog from "components/Dialog";

import { InfoInverted } from "icons";

interface WarnRebootDialogProps {
  onClose: () => void;
  onSubmit: () => void;
  show: boolean;
}

const WarnRebootDialog = ({
  onClose,
  onSubmit,
  show,
}: WarnRebootDialogProps) => {
  return (
    <Dialog
      title="Reboot miner?"
      preventScroll
      titleSize="text-heading-200"
      subtitle="Rebooting your miner will take a few minutes. Do not repair or unplug the miner while it’s rebooting."
      subtitleSize="text-300"
      show={show}
      testId="warn-reboot-dialog"
    >
      <Callout
        className="!px-3 !py-2"
        intent={intents.information}
        title="Miner logs get reset when you reboot your miner so we’ll auto-export your logs before the miner reboots."
        prefixIcon={<InfoInverted />}
      />
      <ButtonGroup
        className="mt-4"
        variant={groupVariants.stack}
        size={sizes.base}
        buttons={[
          {
            text: "Export logs and reboot miner",
            onClick: onSubmit,
            variant: variants.primary,
            testId: "reboot-button",
          },
          {
            text: "Cancel",
            onClick: onClose,
            variant: variants.secondary,
            testId: "cancel-button",
          },
        ]}
      />
    </Dialog>
  );
};

export default WarnRebootDialog;
