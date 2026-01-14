import { useMinerFirmwareVersion } from "@/protoFleet/store";

type MinerFirmwareProps = {
  deviceIdentifier: string;
};

const MinerFirmware = ({ deviceIdentifier }: MinerFirmwareProps) => {
  const firmwareVersion = useMinerFirmwareVersion(deviceIdentifier);
  return <span>{firmwareVersion || "—"}</span>;
};

export default MinerFirmware;
