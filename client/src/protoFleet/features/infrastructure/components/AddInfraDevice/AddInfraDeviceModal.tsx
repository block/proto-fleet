import { useCallback, useState } from "react";

import ManualAddStep from "./ManualAddStep";
import ScanNetworkStep from "./ScanNetworkStep";
import type { DiscoveredInfraDevice } from "@/protoFleet/features/infrastructure/types";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";

interface AddInfraDeviceModalProps {
  discoveredDevices?: DiscoveredInfraDevice[];
  onDismiss: () => void;
  onSuccess: () => void;
}

const AddInfraDeviceModal = ({ discoveredDevices, onDismiss, onSuccess }: AddInfraDeviceModalProps) => {
  const [mode, setMode] = useState("scan");
  const [canPair, setCanPair] = useState(false);
  const [pairHandler, setPairHandler] = useState<(() => void) | null>(null);

  const handleModeSelect = useCallback((key: string) => {
    setMode(key);
    setCanPair(false);
    setPairHandler(null);
  }, []);

  const handleScanSelection = useCallback((count: number, handler: () => void) => {
    setCanPair(count > 0);
    setPairHandler(() => handler);
  }, []);

  const handleManualValid = useCallback((valid: boolean, handler: () => void) => {
    setCanPair(valid);
    setPairHandler(() => handler);
  }, []);

  const pairLabel = mode === "scan" ? "Pair devices" : "Pair device";

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Add infrastructure device"
      buttons={[
        {
          text: pairLabel,
          variant: variants.primary,
          onClick: () => pairHandler?.(),
          disabled: !canPair,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <SegmentedControl
          segments={[
            { key: "scan", title: "Scan network" },
            { key: "manual", title: "Add manually" },
          ]}
          initialSegmentKey={mode}
          onSelect={handleModeSelect}
        />
        {mode === "scan" ? (
          <ScanNetworkStep
            discoveredDevices={discoveredDevices}
            onSuccess={onSuccess}
            onSelectionChange={handleScanSelection}
          />
        ) : (
          <ManualAddStep onSuccess={onSuccess} onValidChange={handleManualValid} />
        )}
      </div>
    </Modal>
  );
};

export default AddInfraDeviceModal;
