import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface WarnSleepDialogProps {
  onClose: () => void;
  onSubmit: () => void;
  open?: boolean;
}

const WarnSleepDialog = ({ onClose, onSubmit, open }: WarnSleepDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Enter sleep mode?"
      preventScroll
      subtitle="Your miner will stop hashing when in sleep mode but will still be powered on. Do not repair a miner when it's in sleep mode."
      subtitleSize="text-300"
      testId="warn-sleep-dialog"
      onDismiss={onClose}
      buttons={[
        {
          text: "Cancel",
          onClick: onClose,
          variant: variants.secondary,
          testId: "cancel-button",
        },
        {
          text: "Enter sleep mode",
          onClick: onSubmit,
          variant: variants.primary,
          testId: "sleep-button",
        },
      ]}
    />
  );
};

export default WarnSleepDialog;
