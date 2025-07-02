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
  return url ? (
    <a href={url} target="_blank" rel="noopener noreferrer">
      {name}
    </a>
  ) : (
    <span>{name}</span>
  );
};

export default MinerName;
