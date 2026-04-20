import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface WarnWakeDialogProps {
  onClose: () => void;
  onSubmit: () => void;
  open?: boolean;
}

const WarnWakeDialog = ({ onClose, onSubmit, open }: WarnWakeDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Wake up miner?"
      preventScroll
      subtitle="This miner is asleep and not hashing. Waking it up will resume normal hashing activity."
      subtitleSize="text-300"
      testId="warn-wake-up-dialog"
      onDismiss={onClose}
      buttons={[
        {
          text: "Cancel",
          onClick: onClose,
          variant: variants.secondary,
          testId: "cancel-button",
        },
        {
          text: "Wake up miner",
          onClick: onSubmit,
          variant: variants.primary,
          testId: "wake-up-button",
        },
      ]}
    />
  );
};

export default WarnWakeDialog;
