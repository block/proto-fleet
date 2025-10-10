import { Immersion } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface ImmersionConfirmationDialogProps {
  onDismiss: () => void;
  onConfirm: () => void;
  show: boolean;
  isLoading?: boolean;
}

const ImmersionConfirmationDialog = ({
  onDismiss,
  onConfirm,
  show,
  isLoading = false,
}: ImmersionConfirmationDialogProps) => {
  return (
    <Dialog
      show={show}
      title="Confirm immersion cooling"
      titleSize="text-heading-200"
      subtitle="Confirming will disable the fans and power down the miner. The fans will not turn on when the miner is powered on again."
      subtitleSize="text-300"
      icon={
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-5">
          <Immersion width={iconSizes.xLarge} />
        </div>
      }
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onDismiss,
          disabled: isLoading,
        },
        {
          text: "Confirm & sleep",
          variant: variants.primary,
          onClick: onConfirm,
          disabled: isLoading,
        },
      ]}
      className="w-[380px]"
    />
  );
};

export default ImmersionConfirmationDialog;
