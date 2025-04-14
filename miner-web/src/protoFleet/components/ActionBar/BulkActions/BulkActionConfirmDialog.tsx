import { ActionWarnDialogOptions } from "../types";
import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";

interface BulkActionConfirmDialogProps {
  actionConfirmation: ActionWarnDialogOptions;
  show: boolean;
  onConfirmation: () => void;
  onCancel: () => void;
  testId: string;
}

const BulkActionConfirmDialog = ({
  actionConfirmation,
  show,
  onConfirmation,
  onCancel,
  testId,
}: BulkActionConfirmDialogProps) => {
  return (
    <Dialog
      className="visible"
      title={actionConfirmation.title}
      preventScroll
      titleSize="text-heading-200"
      subtitle={actionConfirmation.subtitle}
      subtitleSize="text-300"
      show={show}
      testId={testId}
    >
      <ButtonGroup
        className="mt-4"
        variant={groupVariants.stack}
        size={sizes.base}
        buttons={[
          {
            text: actionConfirmation.confirmAction.title,
            onClick: onConfirmation,
            variant: actionConfirmation.confirmAction.variant,
            testId: actionConfirmation.testId,
          },
          {
            text: "Cancel",
            onClick: onCancel,
            variant: variants.secondary,
            testId: "cancel-button",
          },
        ]}
      />
    </Dialog>
  );
};

export default BulkActionConfirmDialog;
