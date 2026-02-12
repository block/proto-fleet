import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerMacAddress } from "@/protoFleet/store";

type MinerMacAddressProps = {
  deviceIdentifier: string;
};

const MinerMacAddress = ({ deviceIdentifier }: MinerMacAddressProps) => {
  const macAddress = useMinerMacAddress(deviceIdentifier);
  return <span>{macAddress || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerMacAddress;
