import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";

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
      testId="warn-backup-pool-dialog"
      buttonGroupVariant={groupVariants.stack}
      buttons={[
        {
          text: "Continue without backup",
          onClick: onContinueWithoutBackup,
          variant: variants.secondary,
          testId: "continue-without-backup-button",
        },
        {
          text: "Add a backup pool",
          onClick: onAddBackupPool,
          variant: variants.primary,
        },
      ]}
    />
  );
};

export default WarnBackupPoolDialog;
