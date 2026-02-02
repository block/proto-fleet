import React, { useState } from "react";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { ProtoFleetStatusModal } from "@/protoFleet/components/StatusModal";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";
import SingleMinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu";
import MinerFrame from "@/protoFleet/features/fleetManagement/components/MinerFrame";
import { useFleetStore, useMiner, useMinerDeviceStatus, useMinerName, useMinerUrl } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { useNeedsAttention } from "@/shared/hooks/useNeedsAttention";

type MinerNameProps = {
  deviceIdentifier: string;
};

const MinerName = ({ deviceIdentifier }: MinerNameProps) => {
  const name = useMinerName(deviceIdentifier) || deviceIdentifier;
  const url = useMinerUrl(deviceIdentifier);
  const miner = useMiner(deviceIdentifier);
  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");
  const [isMinerFrameOpen, setIsMinerFrameOpen] = useState(false);
  const [isStatusModalOpen, setStatusModalOpen] = useState(false);

  // Get errors from normalized store
  const selectErrorsByDevice = useFleetStore((state) => state.fleet.selectErrorsByDevice);
  const errors = selectErrorsByDevice(deviceIdentifier);

  // Compute if miner needs attention
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatusFromStore === DeviceStatus.NEEDS_MINING_POOL;
  const needsAttention = useNeedsAttention(needsAuthentication, needsMiningPool, errors);

  const handleClick = (e: React.MouseEvent<HTMLAnchorElement>) => {
    if (url) {
      try {
        const parsedUrl = new URL(url);
        if (parsedUrl.protocol === "https:") {
          e.preventDefault();
          setIsMinerFrameOpen(true);
        }
      } catch (error) {
        console.error("Invalid URL:", error);
      }
    }
  };

  const handleAlertClick = () => {
    setStatusModalOpen(true);
  };

  return (
    <div className="flex w-full items-center justify-between gap-3">
      <div>
        {url ? (
          <>
            <a href={url} target="_blank" rel="noopener noreferrer" onClick={handleClick}>
              {name}
            </a>
            {isMinerFrameOpen ? (
              <MinerFrame title={name} src={url} onDismiss={() => setIsMinerFrameOpen(false)} />
            ) : null}
          </>
        ) : (
          <span>{name}</span>
        )}
      </div>
      <div className="flex items-center gap-2">
        {needsAttention && !needsAuthentication && (
          <button
            onClick={handleAlertClick}
            className="cursor-pointer transition-opacity hover:opacity-80"
            aria-label="View issues"
          >
            <Alert width="w-4" className="text-red-500" />
          </button>
        )}
        <SingleMinerActionsMenu deviceIdentifier={deviceIdentifier} disabled={needsAuthentication} />
      </div>
      {isStatusModalOpen && !needsAuthentication && needsMiningPool && (
        <PoolSelectionPageWrapper
          selectedMiners={[{ deviceIdentifier, deviceStatus: deviceStatusFromStore }]}
          selectionMode="subset"
          onSuccess={() => setStatusModalOpen(false)}
          onError={() => setStatusModalOpen(false)}
          onDismiss={() => setStatusModalOpen(false)}
        />
      )}
      {isStatusModalOpen && !needsAuthentication && !needsMiningPool && (
        <ProtoFleetStatusModal
          show={isStatusModalOpen}
          onClose={() => setStatusModalOpen(false)}
          deviceId={deviceIdentifier}
        />
      )}
    </div>
  );
};

export default MinerName;
