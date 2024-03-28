import { variants } from "components/Button";
import Modal from "components/Modal";

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
          text: "Repair",
          variant: variants.secondary,
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
