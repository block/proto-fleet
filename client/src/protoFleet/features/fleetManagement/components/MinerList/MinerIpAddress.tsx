import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerIpAddress, useMinerUrl } from "@/protoFleet/store";

type MinerIpAddressProps = {
  deviceIdentifier: string;
};

const MinerIpAddress = ({ deviceIdentifier }: MinerIpAddressProps) => {
  const ipAddress = useMinerIpAddress(deviceIdentifier);
  const url = useMinerUrl(deviceIdentifier);

  if (!ipAddress) {
    return <span>{INACTIVE_PLACEHOLDER}</span>;
  }

  if (!url) {
    return <span>{ipAddress}</span>;
  }

  return (
    <a href={url} target="_blank" rel="noopener noreferrer">
      {ipAddress}
    </a>
  );
};

export default MinerIpAddress;
