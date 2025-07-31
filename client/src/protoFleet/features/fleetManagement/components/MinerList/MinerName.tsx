import React, { useState } from "react";
import MinerFrame from "@/protoFleet/features/fleetManagement/components/MinerFrame";
import {
  useMinerName,
  useMinerUrl,
} from "@/protoFleet/features/fleetManagement/store/useFleetStore";

type MinerNameProps = {
  deviceIdentifier: string;
};

const MinerName = ({ deviceIdentifier }: MinerNameProps) => {
  const name = useMinerName(deviceIdentifier) || deviceIdentifier;
  const url = useMinerUrl(deviceIdentifier);
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

  return url ? (
    <>
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        onClick={handleClick}
      >
        {name}
      </a>
      {isMinerFrameOpen ? (
        <MinerFrame
          title={name}
          src={url}
          onDismiss={() => setIsMinerFrameOpen(false)}
        />
      ) : null}
    </>
  ) : (
    <span>{name}</span>
  );
};

export default MinerName;
