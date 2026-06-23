import { useCallback, useState } from "react";

import ManualAddStep, { type ManualAddStepState } from "./ManualAddStep";
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
  const [canTest, setCanTest] = useState(false);
  const [addHandler, setAddHandler] = useState<(() => void) | null>(null);
  const [testHandler, setTestHandler] = useState<(() => void) | null>(null);
  const [isTesting, setIsTesting] = useState(false);

  const handleManualStateChange = useCallback((state: ManualAddStepState) => {
    setCanAdd(state.canAdd);
    setCanTest(state.canTest);
    setAddHandler(() => state.addHandler);
    setTestHandler(() => state.testHandler);
  }, []);

  const handleTestConnection = useCallback(() => {
    if (!testHandler) return;
    setIsTesting(true);
    setTimeout(() => {
      testHandler();
      setIsTesting(false);
    }, 1200);
  }, [testHandler]);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Add infrastructure device"
      description="Add a single fan or fan group controlled through a bridge or PLC."
      buttons={[
        {
          text: "Test connection",
          variant: variants.secondary,
          onClick: handleTestConnection,
          disabled: !canTest,
          loading: isTesting,
          dismissModalOnClick: false,
        },
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
        onStateChange={handleManualStateChange}
      />
    </Modal>
  );
};

export default AddInfraDeviceModal;
