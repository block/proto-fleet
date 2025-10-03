import { immersionModeInstructionSteps } from "./constants";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import SlotNumber from "@/shared/components/SlotNumber";

interface ImmersionConfirmationModalProps {
  onDismiss: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
}

const ImmersionConfirmationModal = ({
  onDismiss,
  onConfirm,
  isLoading = false,
}: ImmersionConfirmationModalProps) => {
  return (
    <Modal
      buttons={[
        {
          text: "Confirm & sleep",
          variant: variants.primary,
          onClick: onConfirm,
          disabled: isLoading,
        },
      ]}
      title="Immersion cooling"
      onDismiss={onDismiss}
      preventClose={isLoading}
      size="small"
    >
      <div className="mt-6 flex flex-col gap-6">
        <Header
          title="Prepare your rig"
          titleSize="text-heading-300"
          subtitle={`Switching to immersion cooling requires the miner to be manually rebooted to ensure safe fan removal and prevent damage. Confirming will put the miner to sleep, and the following steps will need to be performed.`}
          subtitleSize="text-300"
          icon={
            <Alert className="text-text-emphasis" width={iconSizes.xLarge} />
          }
        />
        <div>
          {immersionModeInstructionSteps.map((step, index) => (
            <Row
              key={index}
              divider={false}
              prefixIcon={<SlotNumber number={index + 1} />}
            >
              <span className="text-300 text-text-primary">{step}</span>
            </Row>
          ))}
        </div>
      </div>
    </Modal>
  );
};

export default ImmersionConfirmationModal;
