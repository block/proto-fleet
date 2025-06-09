import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface WarnDiscardDialogProps {
  continueEditing: () => void;
  onDiscard: () => void;
  show: boolean;
}

const WarnDiscardDialog = ({
  continueEditing,
  onDiscard,
  show,
}: WarnDiscardDialogProps) => {
  return (
    <Dialog
      title="Discard changes?"
      subtitle="You have unsaved changes that will be lost."
      subtitleSize="text-300"
      preventScroll
      titleSize="text-heading-200"
      show={show}
      testId="warn-discard-dialog"
      buttons={[
        {
          text: "Discard changes",
          onClick: onDiscard,
          variant: variants.secondaryDanger,
          testId: "discard-changes-button",
        },
        {
          text: "Continue editing",
          onClick: continueEditing,
          variant: variants.primary,
          testId: "continue-editing-button",
        },
      ]}
    />
  );
};

export default WarnDiscardDialog;
