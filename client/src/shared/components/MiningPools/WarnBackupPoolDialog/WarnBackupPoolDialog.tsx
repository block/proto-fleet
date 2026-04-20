import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";

interface WarnBackupPoolDialogProps {
  onAddBackupPool: () => void;
  onContinueWithoutBackup: () => void;
  open?: boolean;
}

const WarnBackupPoolDialog = ({ onAddBackupPool, onContinueWithoutBackup, open }: WarnBackupPoolDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Continue without a backup pool?"
      subtitle="Adding a backup pool will help this miner keep mining if your default pool fails."
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
