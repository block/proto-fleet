import { Info } from "@/shared/assets/icons";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface NoFansDetectedDialogProps {
  onUseAirCooling: () => void;
  onConfirmImmersionCooling: () => void;
  loading?: boolean;
  open?: boolean;
}

const NoFansDetectedDialog = ({
  onUseAirCooling,
  onConfirmImmersionCooling,
  loading,
  open,
}: NoFansDetectedDialogProps) => {
  return (
    <Dialog
      open={open}
      title="No fans detected"
      subtitle="No fans are detected for this miner, will it be configured to use immersion cooling?"
      subtitleSize="text-300"
      icon={
        <DialogIcon>
          <Info />
        </DialogIcon>
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
