import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";
import NetworkDetails from "@/shared/components/Setup/NetworkDetails";

interface NetworkConfirmationDialogProps {
  gateway?: string;
  subnet?: string;
  show: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

const NetworkConfirmationDialog = ({
  gateway,
  subnet,
  show,
  onCancel,
  onConfirm,
}: NetworkConfirmationDialogProps) => {
  return (
    <Dialog
      show={show}
      className="!p-0 tablet:!w-108 laptop:!w-108 desktop:!w-108"
      headerClassName="pt-6 px-6"
      title="Confirm the network you’re connected to before continuing"
      titleSize="text-heading-200"
      subtitle="The miners are configured and connected to your local network. To ensure a smooth setup process, please verify that the network displayed below is the one to which you intend to add the miners."
      subtitleSize="text-text-300"
      animate={false}
    >
      <div className="px-6">
        <NetworkDetails subnet={subnet} gateway={gateway} />
      </div>
      <ButtonGroup
        className="mt-6 rounded-b-3xl bg-surface-5 p-6"
        variant={groupVariants.justifyBetween}
        size={sizes.base}
        buttons={[
          {
            text: "Cancel",
            onClick: onCancel,
            variant: variants.secondary,
          },
          {
            text: "Confirm and continue",
            onClick: onConfirm,
            variant: variants.accent,
          },
        ]}
      />
    </Dialog>
  );
};

export default NetworkConfirmationDialog;
