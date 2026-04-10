import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface WarnRebootDialogProps {
  onClose: () => void;
  onSubmit: () => void;
  open?: boolean;
}

const WarnRebootDialog = ({ onClose, onSubmit, open }: WarnRebootDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Reboot miner?"
      preventScroll
      subtitle="Rebooting your miner will take a few minutes. Do not repair or unplug the miner while it's rebooting."
      subtitleSize="text-300"
      testId="warn-reboot-dialog"
      onDismiss={onClose}
      buttons={[
        {
          text: "Cancel",
          onClick: onClose,
          variant: variants.secondary,
          testId: "cancel-button",
        },
        {
          text: "Reboot miner",
          onClick: onSubmit,
          variant: variants.primary,
          testId: "reboot-button",
        },
      ]}
    />
  );
};

export default WarnRebootDialog;
