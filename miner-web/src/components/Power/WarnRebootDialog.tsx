import { variants } from "components/Button";
import ButtonGroup, { groupVariants, sizes } from "components/ButtonGroup";
import Dialog from "components/Dialog";

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
      subtitle="Rebooting a miner takes a few minutes. While the miner is rebooting, you will not be able to make any changes to its settings or performance."
      subtitleSize="text-300"
      show={show}
      testId="warn-reboot-dialog"
    >
      <ButtonGroup
        className="mt-4"
        variant={groupVariants.stack}
        size={sizes.base}
        buttons={[
          {
            text: "Reboot miner",
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
