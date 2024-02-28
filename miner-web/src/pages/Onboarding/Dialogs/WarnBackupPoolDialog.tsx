import { variants } from "components/Button";
import ButtonGroup, { groupVariants, sizes } from "components/ButtonGroup";
import Dialog from "components/Dialog";

interface WarnBackupPoolDialogProps {
  onAddBackupPool: () => void;
  onContinueWithoutBackup: () => void;
  show: boolean;
}

const WarnBackupPoolDialog = ({
  onAddBackupPool,
  onContinueWithoutBackup,
  show,
}: WarnBackupPoolDialogProps) => {
  return (
    <Dialog
      show={show}
      title="Continue without a backup pool?"
      subtitle="Adding a backup pool will help this miner keep mining if your default pool fails."
      titleSize="text-heading-200"
    >
      <ButtonGroup
        variant={groupVariants.stack}
        size={sizes.base}
        buttons={[
          {
            text: "Add a backup pool",
            onClick: onAddBackupPool,
            variant: variants.primary,
          },
          {
            text: "Continue without backup",
            onClick: onContinueWithoutBackup,
            variant: variants.secondary,
          },
        ]}
      />
    </Dialog>
  );
};

export default WarnBackupPoolDialog;
