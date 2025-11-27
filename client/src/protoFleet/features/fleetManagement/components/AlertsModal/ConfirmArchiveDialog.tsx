import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface ConfirmArchiveDialogProps {
  show: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

const ConfirmArchiveDialog = ({ show, onConfirm, onCancel }: ConfirmArchiveDialogProps) => {
  return (
    <Dialog
      title="Archive all alerts?"
      subtitle="You will still be able to view archived alerts."
      preventScroll
      show={show}
      buttons={[
        {
          text: "Cancel",
          onClick: onCancel,
          variant: variants.secondary,
        },
        {
          text: "Archive alerts",
          onClick: onConfirm,
          variant: variants.accent,
        },
      ]}
    />
  );
};

export default ConfirmArchiveDialog;
