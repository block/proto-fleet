import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface FansDetectedDialogProps {
  onRetry: () => void;
  onCancel: () => void;
  show: boolean;
  isLoading?: boolean;
}

const FansDetectedDialog = ({ onRetry, onCancel, show, isLoading = false }: FansDetectedDialogProps) => {
  return (
    <Dialog
      show={show}
      title="Fans detected"
      titleSize="text-heading-200"
      subtitle="Fans are detected for this miner. Remove fans to continue in immersion mode, or switch back to air cooled mode."
      subtitleSize="text-300"
      icon={
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-5">
          <Alert className="text-text-critical" width={iconSizes.medium} />
        </div>
      }
      buttons={[
        {
          text: "Use air cooled mode",
          variant: variants.secondary,
          onClick: onCancel,
          disabled: isLoading,
        },
        {
          text: "Try again",
          variant: variants.secondary,
          onClick: onRetry,
          disabled: isLoading,
          loading: isLoading,
        },
      ]}
    />
  );
};

export default FansDetectedDialog;
