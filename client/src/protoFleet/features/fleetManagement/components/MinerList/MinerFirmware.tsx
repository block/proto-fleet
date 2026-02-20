import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerFirmwareVersion } from "@/protoFleet/store";

type MinerFirmwareProps = {
  deviceIdentifier: string;
};

const MinerFirmware = ({ deviceIdentifier }: MinerFirmwareProps) => {
  const firmwareVersion = useMinerFirmwareVersion(deviceIdentifier);
  return <span>{firmwareVersion ?? INACTIVE_PLACEHOLDER}</span>;
};

export default MinerFirmware;
