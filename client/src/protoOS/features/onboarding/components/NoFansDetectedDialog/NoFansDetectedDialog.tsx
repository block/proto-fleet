import { Info } from "@/shared/assets/icons";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";

interface NoFansDetectedDialogProps {
  show: boolean;
  onUseAirCooling: () => void;
  onConfirmImmersionCooling: () => void;
  loading?: boolean;
}

const NoFansDetectedDialog = ({
  show,
  onUseAirCooling,
  onConfirmImmersionCooling,
  loading,
}: NoFansDetectedDialogProps) => {
  return (
    <Dialog
      show={show}
      title="No fans detected"
      subtitle="No fans are detected for this miner, will it be configured to use immersion cooling?"
      titleSize="text-heading-300"
      subtitleSize="text-300"
      icon={
        <div className="flex size-10 items-center justify-center rounded-lg bg-surface-5">
          <Info />
        </div>
      }
      buttonGroupVariant={groupVariants.justifyBetween}
      loading={loading}
      buttons={[
        {
          text: "Use air cooling",
          onClick: onUseAirCooling,
          variant: "secondary",
        },
        {
          text: "Confirm immersion cooling",
          onClick: onConfirmImmersionCooling,
          variant: "primary",
        },
      ]}
    />
  );
};

export default NoFansDetectedDialog;
