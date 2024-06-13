import { variants } from "components/Button";
import ButtonGroup, { groupVariants, sizes } from "components/ButtonGroup";
import Dialog from "components/Dialog";

interface WarnSleepDialogProps {
  onClose: () => void;
  onSubmit: () => void;
  show: boolean;
}

const WarnSleepDialog = ({ onClose, onSubmit, show }: WarnSleepDialogProps) => {
  return (
    <Dialog
      title="Enter sleep mode?"
      preventScroll
      titleSize="text-heading-200"
      subtitle="Your miner will stop hashing when in sleep mode but will still be powered on. Do not repair a miner when it’s in sleep mode."
      subtitleSize="text-300"
      show={show}
      testId="warn-sleep-dialog"
    >
      <ButtonGroup
        className="mt-4"
        variant={groupVariants.stack}
        size={sizes.base}
        buttons={[
          {
            text: "Enter sleep mode",
            onClick: onSubmit,
            variant: variants.primary,
            testId: "sleep-button",
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

export default WarnSleepDialog;
