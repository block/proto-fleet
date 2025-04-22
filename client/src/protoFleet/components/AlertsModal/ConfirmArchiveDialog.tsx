import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";

interface ConfirmArchiveDialogProps {
  show: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

const ConfirmArchiveDialog = ({
  show,
  onConfirm,
  onCancel,
}: ConfirmArchiveDialogProps) => {
  return (
    <Dialog
      title="Archive all alerts?"
      subtitle="You will still be able to view archived alerts."
      preventScroll
      show={show}
    >
      <ButtonGroup
        className="mt-4"
        variant={groupVariants.fill}
        size={sizes.base}
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
    </Dialog>
  );
};

export default ConfirmArchiveDialog;
