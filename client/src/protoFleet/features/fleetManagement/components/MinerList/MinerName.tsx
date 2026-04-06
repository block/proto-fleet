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
  const hasFirmwareStatus = deviceStatus === DeviceStatus.UPDATING || deviceStatus === DeviceStatus.REBOOT_REQUIRED;
  const needsAttention = useNeedsAttention(needsAuthentication, needsMiningPool, errors, false, hasFirmwareStatus);

  const handleNameClick = (e: React.MouseEvent) => {
    const row = (e.currentTarget as HTMLElement).closest("tr");
    const checkbox = row?.querySelector<HTMLInputElement>('input[type="checkbox"]');
    if (checkbox && !checkbox.disabled) {
      e.stopPropagation();
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
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <button
        type="button"
        className="min-w-0 cursor-pointer truncate text-left"
        title={name}
        onClick={handleNameClick}
      >
        {name}
      </button>
      <div className="flex items-center gap-2">
        {needsAttention && !needsAuthentication && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              onOpenStatusFlow(deviceIdentifier);
            }}
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
