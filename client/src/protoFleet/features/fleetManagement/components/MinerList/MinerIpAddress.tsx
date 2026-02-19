import React, { useState } from "react";
import { INACTIVE_PLACEHOLDER } from "./constants";
import MinerFrame from "@/protoFleet/features/fleetManagement/components/MinerFrame";
import { useMinerIpAddress, useMinerName, useMinerUrl } from "@/protoFleet/store";

type MinerIpAddressProps = {
  deviceIdentifier: string;
};

const MinerIpAddress = ({ deviceIdentifier }: MinerIpAddressProps) => {
  const ipAddress = useMinerIpAddress(deviceIdentifier);
  const url = useMinerUrl(deviceIdentifier);
  const name = useMinerName(deviceIdentifier) || deviceIdentifier;
  const [isMinerFrameOpen, setIsMinerFrameOpen] = useState(false);

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

  if (!ipAddress) {
    return <span>{INACTIVE_PLACEHOLDER}</span>;
  }

  if (!url) {
    return <span>{ipAddress}</span>;
  }

  return (
    <>
      <a href={url} target="_blank" rel="noopener noreferrer" onClick={handleClick}>
        {ipAddress}
      </a>
      {isMinerFrameOpen ? <MinerFrame title={name} src={url} onDismiss={() => setIsMinerFrameOpen(false)} /> : null}
    </>
  );
};

export default MinerIpAddress;
