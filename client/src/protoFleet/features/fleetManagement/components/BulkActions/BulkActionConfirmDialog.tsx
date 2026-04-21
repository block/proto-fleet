import { ActionWarnDialogOptions } from "./types";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface BulkActionConfirmDialogProps {
  open?: boolean;
  actionConfirmation: ActionWarnDialogOptions;
  onConfirmation: () => void;
  onCancel: () => void;
  testId: string;
}

const BulkActionConfirmDialog = ({
  open,
  actionConfirmation,
  onConfirmation,
  onCancel,
  testId,
}: BulkActionConfirmDialogProps) => {
  return (
    <Dialog
      open={open}
      className="visible"
      title={actionConfirmation.title}
      preventScroll
      subtitle={actionConfirmation.subtitle}
      subtitleSize="text-300"
      testId={testId}
      onDismiss={onCancel}
      buttons={[
        {
          text: "Cancel",
          onClick: onCancel,
          variant: variants.secondary,
          testId: "cancel-button",
        },
        {
          text: actionConfirmation.confirmAction.title,
          onClick: onConfirmation,
          variant: actionConfirmation.confirmAction.variant,
          testId: actionConfirmation.testId,
        },
      ]}
    />
  );
};

export default BulkActionConfirmDialog;
