import { useMinerName } from "@/protoFleet/features/fleetManagement/store/useFleetStore";

type MinerNameProps = {
  deviceIdentifier: string;
};

const MinerName = ({ deviceIdentifier }: MinerNameProps) => {
  const name = useMinerName(deviceIdentifier);
  return <span>{name || deviceIdentifier}</span>;
};

export default MinerName;
