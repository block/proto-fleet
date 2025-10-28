import { useMinerIpAddress } from "@/protoFleet/store";

type MinerIpAddressProps = {
  deviceIdentifier: string;
};

const MinerIpAddress = ({ deviceIdentifier }: MinerIpAddressProps) => {
  const ipAddress = useMinerIpAddress(deviceIdentifier);
  return <span>{ipAddress || "-"}</span>;
};

export default MinerIpAddress;
