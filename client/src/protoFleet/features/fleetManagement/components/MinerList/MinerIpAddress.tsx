import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerIpAddress } from "@/protoFleet/store";

type MinerIpAddressProps = {
  deviceIdentifier: string;
};

const MinerIpAddress = ({ deviceIdentifier }: MinerIpAddressProps) => {
  const ipAddress = useMinerIpAddress(deviceIdentifier);
  return <span>{ipAddress || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerIpAddress;
