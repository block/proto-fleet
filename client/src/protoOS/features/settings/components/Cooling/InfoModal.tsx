import { immersionModeInstructionSteps } from "./constants";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import SlotNumber from "@/shared/components/SlotNumber";

interface InfoModalProps {
  onDismiss: () => void;
  buttons?: ButtonProps[] | undefined;
}

const InfoModal = ({ onDismiss, buttons }: InfoModalProps) => {
  return (
    <Modal title="Immersion cooling" onDismiss={onDismiss} buttons={buttons}>
      <div className="mt-6 flex flex-col gap-6">
        <Header
          title="Prepare your miner for immersion"
          titleSize="text-heading-300"
          subtitle={`To prepare for immersion, your miner will be put into sleep mode—hashing will be paused and fans will be disabled to avoid hardware damage. Once the miner is asleep, follow these steps:`}
          subtitleSize="text-300"
          icon={<Info width={iconSizes.xLarge} className="text-core-primary-fill" />}
        />
        <div>
          {immersionModeInstructionSteps.map((step, index) => (
            <Row key={index} divider={false} prefixIcon={<SlotNumber number={index + 1} />}>
              <div className="text-emphasis-300 text-text-primary">{step.title}</div>
              <div className="text-200 text-text-primary-70">{step.subtitle}</div>
            </Row>
          ))}
        </div>
      </div>
    </Modal>
  );
};

export default InfoModal;
