import MinerMeasurement from "./MinerMeasurement";
import { useMinerTemperature } from "@/protoFleet/features/fleetManagement/store/useFleetStore";

type MinerTemperatureProps = {
  deviceIdentifier: string;
};

const MinerTemperature = ({ deviceIdentifier }: MinerTemperatureProps) => {
  const temperature = useMinerTemperature(deviceIdentifier);
  return <MinerMeasurement measurement={temperature} unit="°C" />;
};

export default MinerTemperature;
