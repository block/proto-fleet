import React from "react";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import SingleMinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu";
import { useFleetStore, useMiner, useMinerDeviceStatus, useMinerName } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { useNeedsAttention } from "@/shared/hooks/useNeedsAttention";

type MinerNameProps = {
  deviceIdentifier: string;
  onOpenStatusFlow: (deviceIdentifier: string) => void;
};

const MinerName = ({ deviceIdentifier, onOpenStatusFlow }: MinerNameProps) => {
  const name = useMinerName(deviceIdentifier) || deviceIdentifier;
  const miner = useMiner(deviceIdentifier);
  const deviceStatus = useMinerDeviceStatus(deviceIdentifier || "");

  const selectErrorsByDevice = useFleetStore((state) => state.fleet.selectErrorsByDevice);
  const errors = selectErrorsByDevice(deviceIdentifier);

  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatus === DeviceStatus.NEEDS_MINING_POOL;
  const needsAttention = useNeedsAttention(needsAuthentication, needsMiningPool, errors);

  const handleNameClick = (e: React.MouseEvent) => {
    const row = (e.currentTarget as HTMLElement).closest("tr");
    const checkbox = row?.querySelector<HTMLInputElement>('input[type="checkbox"]');
    if (checkbox && !checkbox.disabled) {
      checkbox.dispatchEvent(
        new MouseEvent("click", {
          bubbles: true,
          cancelable: true,
          shiftKey: e.shiftKey,
          ctrlKey: e.ctrlKey,
          metaKey: e.metaKey,
        }),
      );
    }
  };

  return (
    <div className="flex w-full items-center justify-between gap-3">
      <div>
        <button type="button" className="cursor-pointer" onClick={handleNameClick}>
          {name}
        </button>
      </div>
      <div className="flex items-center gap-2">
        {needsAttention && !needsAuthentication && (
          <button
            onClick={() => onOpenStatusFlow(deviceIdentifier)}
            className="cursor-pointer transition-opacity hover:opacity-80"
            aria-label="View issues"
          >
            <Alert width="w-4" className="text-red-500" />
          </button>
        )}
        <SingleMinerActionsMenu deviceIdentifier={deviceIdentifier} disabled={needsAuthentication} />
      </div>
    </div>
  );
};

export default MinerName;
