import { useCallback, useState } from "react";

import ManualAddStep from "./ManualAddStep";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface AddInfraDeviceModalProps {
  onDismiss: () => void;
  onSuccess: () => void;
  siteOptions?: string[];
  buildingOptions?: string[];
  buildingOptionsBySite?: Record<string, string[]>;
}

const AddInfraDeviceModal = ({
  onDismiss,
  onSuccess,
  siteOptions = [],
  buildingOptions = [],
  buildingOptionsBySite = {},
}: AddInfraDeviceModalProps) => {
  const [canAdd, setCanAdd] = useState(false);
  const [addHandler, setAddHandler] = useState<(() => void) | null>(null);

  const handleManualValid = useCallback((valid: boolean, handler: () => void) => {
    setCanAdd(valid);
    setAddHandler(() => handler);
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
        buildingOptionsBySite={buildingOptionsBySite}
        onSuccess={onSuccess}
        onValidChange={handleManualValid}
      />
    </Modal>
  );
};

export default AddInfraDeviceModal;
