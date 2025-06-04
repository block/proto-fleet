import MinerMeasurement from "./MinerMeasurement";
import { useMinerEfficiency } from "@/protoFleet/features/fleetManagement/store/useFleetStore";

type MinerEfficiencyProps = {
  deviceIdentifier: string;
};

const MinerEfficiency = ({ deviceIdentifier }: MinerEfficiencyProps) => {
  const efficiency = useMinerEfficiency(deviceIdentifier);
  return <MinerMeasurement measurement={efficiency} unit="J/TH" />;
};

export default MinerEfficiency;
