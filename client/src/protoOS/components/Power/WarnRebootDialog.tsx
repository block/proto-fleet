import { InfoInverted } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";

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
      buttons={[
        {
          text: "Cancel",
          onClick: onClose,
          variant: variants.secondary,
          testId: "cancel-button",
        },
        {
          text: "Export logs and reboot miner",
          onClick: onSubmit,
          variant: variants.primary,
          testId: "reboot-button",
        },
      ]}
    >
      <Callout
        className="px-3! py-2!"
        intent={intents.information}
        title="Miner logs get reset when you reboot your miner so we’ll auto-export your logs before the miner reboots."
        prefixIcon={<InfoInverted />}
      />
    </Dialog>
  );
};

export default WarnRebootDialog;
