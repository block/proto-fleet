import { immersionModeInstructionSteps } from "./constants";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import SlotNumber from "@/shared/components/SlotNumber";

interface ImmersionLearnMoreModalProps {
  onDismiss: () => void;
}

const ImmersionLearnMoreModal = ({
  onDismiss,
}: ImmersionLearnMoreModalProps) => {
  return (
    <Modal title="Immersion cooling" onDismiss={onDismiss} size="small">
      <div className="mt-6 flex flex-col gap-6">
        <Header
          title="Prepare your miner for immersion"
          titleSize="text-heading-300"
          subtitle={`Switching to immersion cooling requires the miner to be manually rebooted to ensure safe fan removal and prevent damage. It is critical to perform the following steps.`}
          subtitleSize="text-300"
          icon={
            <Info width={iconSizes.xLarge} className="text-core-primary-fill" />
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

export default ImmersionLearnMoreModal;
