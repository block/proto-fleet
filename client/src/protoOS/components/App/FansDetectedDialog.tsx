import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface FansDetectedDialogProps {
  onContinue: () => void;
  onSwitchToAirCooled: () => void;
  show: boolean;
  isLoading?: boolean;
}

const FansDetectedDialog = ({ onContinue, onSwitchToAirCooled, show, isLoading = false }: FansDetectedDialogProps) => {
  return (
    <Dialog
      show={show}
      title="Fans are disabled"
      titleSize="text-heading-300"
      subtitle="While in immersion mode, fans and fan errors will be disabled. To use fans to cool this miner, switch to air cooling mode."
      subtitleSize="text-300"
      icon={
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-5">
          <Alert className="text-text-critical" width={iconSizes.medium} />
        </div>
      }
      buttons={[
        {
          text: "Switch to air cooling",
          variant: variants.secondary,
          onClick: onSwitchToAirCooled,
          disabled: isLoading,
          loading: isLoading,
        },
        {
          text: "Continue",
          variant: variants.secondary,
          onClick: onContinue,
          disabled: isLoading,
        },
      ]}
    />
  );
};

export default FansDetectedDialog;
