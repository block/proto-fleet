import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";

interface WarnWakeDialogProps {
  onClose: () => void;
  onSubmit: () => void;
  show: boolean;
}

const WarnWakeDialog = ({ onClose, onSubmit, show }: WarnWakeDialogProps) => {
  return (
    <Dialog
      title="Wake up miner?"
      preventScroll
      titleSize="text-heading-200"
      subtitle="This miner is asleep and not hashing. Waking it up will resume normal hashing activity."
      subtitleSize="text-300"
      show={show}
      testId="warn-wake-up-dialog"
    >
      <ButtonGroup
        className="mt-4"
        variant={groupVariants.stack}
        size={sizes.base}
        buttons={[
          {
            text: "Wake up miner",
            onClick: onSubmit,
            variant: variants.primary,
            testId: "wake-up-button",
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

export default WarnWakeDialog;
