import React, { useState } from "react";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import SingleMinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu";
import MinerFrame from "@/protoFleet/features/fleetManagement/components/MinerFrame";
import { useMiner, useMinerName, useMinerUrl } from "@/protoFleet/store";

type MinerNameProps = {
  deviceIdentifier: string;
};

const MinerName = ({ deviceIdentifier }: MinerNameProps) => {
  const name = useMinerName(deviceIdentifier) || deviceIdentifier;
  const url = useMinerUrl(deviceIdentifier);
  const miner = useMiner(deviceIdentifier);
  const [isMinerFrameOpen, setIsMinerFrameOpen] = useState(false);

  // Don't show actions menu for miners requiring authentication (disabled rows)
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;

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

  return (
    <div className="flex w-full items-center justify-between">
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
      <SingleMinerActionsMenu deviceIdentifier={deviceIdentifier} disabled={needsAuthentication} />
    </div>
  );
};

export default MinerName;
