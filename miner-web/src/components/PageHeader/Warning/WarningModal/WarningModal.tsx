import { variants } from "components/Button";
import Modal from "components/Modal";

import { ArrowRight } from "icons";

import AsicModalContent from "./AsicModalContent";
import FanModalContent from "./FanModalContent";

interface PowerUsageModalProps {
  onDismiss: () => void;
  type: "fan" | "asic";
}

const WarningModal = ({ onDismiss, type }: PowerUsageModalProps) => {
  return (
    <Modal
      buttons={[
        {
          text: "Repair instructions",
          variant: variants.secondary,
          suffixIcon: <ArrowRight />,
          // TODO: link to repair page when available
        },
        {
          text: "Done",
          variant: variants.primary,
        },
      ]}
      onDismiss={onDismiss}
    >
      {type === "asic" && <AsicModalContent onDismiss={onDismiss} />}
      {type === "fan" && <FanModalContent />}
    </Modal>
  );
};

export default WarningModal;
