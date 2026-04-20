import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface FansDetectedDialogProps {
  onContinue: () => void;
  onSwitchToAirCooled: () => void;
  isLoading?: boolean;
  open?: boolean;
}

const FansDetectedDialog = ({ onContinue, onSwitchToAirCooled, isLoading = false, open }: FansDetectedDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Fans are disabled"
      subtitle="While in immersion mode, fans and fan errors will be disabled. To use fans to cool this miner, switch to air cooling mode."
      subtitleSize="text-300"
      icon={
        <DialogIcon>
          <Alert className="text-text-critical" width={iconSizes.medium} />
        </DialogIcon>
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
