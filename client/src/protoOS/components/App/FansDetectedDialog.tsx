import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface FansDetectedDialogProps {
  onConfirmImmersion: () => void;
  onSwitchToAirCooled: () => void;
  show: boolean;
  isLoading?: boolean;
}

const FansDetectedDialog = ({
  onConfirmImmersion,
  onSwitchToAirCooled,
  show,
  isLoading = false,
}: FansDetectedDialogProps) => {
  return (
    <Dialog
      show={show}
      title="Fans detected"
      titleSize="text-heading-200"
      subtitle="Fans are detected for this miner, are you sure you want to continue with immersion cooling?"
      subtitleSize="text-300"
      icon={<Alert className="text-text-emphasis" width={iconSizes.xLarge} />}
      buttons={[
        {
          text: "Use air cooled",
          variant: variants.secondary,
          onClick: onSwitchToAirCooled,
          disabled: isLoading,
        },
        {
          text: "Confirm immersion cooling",
          variant: variants.primary,
          onClick: onConfirmImmersion,
          disabled: isLoading,
        },
      ]}
      className="w-[380px]"
    />
  );
};

export default FansDetectedDialog;
