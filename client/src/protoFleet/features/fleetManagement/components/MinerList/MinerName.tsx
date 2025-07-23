import { useState } from "react";
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

  return url ? (
    <>
      <button onClick={() => setIsMinerFrameOpen(true)}>
        <span>{name}</span>
      </button>
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
