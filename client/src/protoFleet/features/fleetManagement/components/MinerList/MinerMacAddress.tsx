import { useMinerMacAddress } from "@/protoFleet/store";

type MinerMacAddressProps = {
  deviceIdentifier: string;
};

const MinerMacAddress = ({ deviceIdentifier }: MinerMacAddressProps) => {
  const macAddress = useMinerMacAddress(deviceIdentifier);
  return <span>{macAddress || "-"}</span>;
};

export default MinerMacAddress;
