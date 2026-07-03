import { useCallback, useState } from "react";

import ManualAddStep, { type ManualAddStepState } from "./ManualAddStep";
import type { InfraBuildingOption, InfraDeviceDraft } from "@/protoFleet/features/infrastructure/types";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface AddInfraDeviceModalProps {
  siteOptions?: string[];
  buildingOptions?: InfraBuildingOption[];
  onDismiss: () => void;
  onSuccess: (device: InfraDeviceDraft) => void;
}

const AddInfraDeviceModal = ({
  siteOptions = [],
  buildingOptions = [],
  onDismiss,
  onSuccess,
}: AddInfraDeviceModalProps) => {
  const [canAdd, setCanAdd] = useState(false);
  const [addHandler, setAddHandler] = useState<(() => void) | null>(null);

  const handleManualStateChange = useCallback((state: ManualAddStepState) => {
    setCanAdd(state.canAdd);
    setAddHandler(() => state.addHandler);
  }, []);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Add infrastructure device"
      description="Add a single fan or fan group controlled through a bridge or PLC."
      buttons={[
        {
          text: "Add device",
          variant: variants.primary,
          onClick: () => addHandler?.(),
          disabled: !canAdd,
          dismissModalOnClick: false,
        },
      ]}
    >
      <ManualAddStep
        siteOptions={siteOptions}
        buildingOptions={buildingOptions}
        onSuccess={onSuccess}
        onStateChange={handleManualStateChange}
      />
    </Modal>
  );
};

export default AddInfraDeviceModal;
