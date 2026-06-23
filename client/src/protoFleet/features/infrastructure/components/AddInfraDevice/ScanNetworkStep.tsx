import { useCallback, useEffect, useState } from "react";

import type { DiscoveredInfraDevice } from "@/protoFleet/features/infrastructure/types";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import { DataNullState } from "@/shared/components/DataNullState";

interface ScanNetworkStepProps {
  discoveredDevices?: DiscoveredInfraDevice[];
  onSuccess: () => void;
  onSelectionChange: (count: number, pairHandler: () => void) => void;
}

interface SelectableDiscoveredDevice extends DiscoveredInfraDevice {
  selected: boolean;
}

const ScanNetworkStep = ({ discoveredDevices = [], onSuccess, onSelectionChange }: ScanNetworkStepProps) => {
  const [isScanning, setIsScanning] = useState(false);
  const [hasScanned, setHasScanned] = useState(false);
  const [devices, setDevices] = useState<SelectableDiscoveredDevice[]>([]);

  const handleScan = useCallback(() => {
    setIsScanning(true);
    setTimeout(() => {
      setIsScanning(false);
      setHasScanned(true);
      setDevices(discoveredDevices.map((d) => ({ ...d, selected: false })));
    }, 2000);
  }, [discoveredDevices]);

  const toggleDevice = useCallback((index: number) => {
    setDevices((prev) => prev.map((d, i) => (i === index ? { ...d, selected: !d.selected } : d)));
  }, []);

  const toggleAll = useCallback(() => {
    setDevices((prev) => {
      const allSelected = prev.every((d) => d.selected);
      return prev.map((d) => ({ ...d, selected: !allSelected }));
    });
  }, []);

  const handlePair = useCallback(() => {
    onSuccess();
  }, [onSuccess]);

  const selectedCount = devices.filter((d) => d.selected).length;

  useEffect(() => {
    onSelectionChange(selectedCount, handlePair);
  }, [selectedCount, handlePair, onSelectionChange]);

  if (!hasScanned) {
    return (
      <div className="flex flex-col items-center gap-4 py-8">
        <span className="text-300 text-text-primary-70">Scan the local network to discover infrastructure devices</span>
        <Button
          text="Start scan"
          variant={variants.primary}
          size={buttonSizes.compact}
          onClick={handleScan}
          loading={isScanning}
        />
      </div>
    );
  }

  if (devices.length === 0) {
    return <DataNullState title="No devices found" description="Try scanning again or add a device manually." />;
  }

  return (
    <div className="flex flex-col">
      <div className="border-b border-border-5">
        <div className="grid grid-cols-[auto_1fr_1fr_1fr] items-center gap-4 px-2 py-2 text-200 font-medium text-text-primary-70">
          <Checkbox
            checked={devices.length > 0 ? devices.every((d) => d.selected) : false}
            partiallyChecked={devices.some((d) => d.selected) ? !devices.every((d) => d.selected) : false}
            onChange={toggleAll}
          />
          <span>Device</span>
          <span>IP Address</span>
          <span>Type</span>
        </div>
      </div>
      {devices.map((device, i) => (
        <label
          key={i}
          className="grid cursor-pointer grid-cols-[auto_1fr_1fr_1fr] items-center gap-4 border-b border-border-5 px-2 py-3 text-300 hover:bg-surface-base"
        >
          <Checkbox checked={device.selected} onChange={() => toggleDevice(i)} />
          <span className="font-medium">{device.name}</span>
          <span className="text-text-primary-70">{device.ipAddress}</span>
          <span className="text-text-primary-70">{device.subtype}</span>
        </label>
      ))}
    </div>
  );
};

export default ScanNetworkStep;
