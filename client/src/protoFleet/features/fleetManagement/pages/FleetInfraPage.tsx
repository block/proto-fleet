import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";

interface FleetInfraPageProps {
  devices?: InfraDeviceItem[];
}

const FleetInfraPage = ({ devices }: FleetInfraPageProps) => {
  return <InfraDeviceList devices={devices} />;
};

export default FleetInfraPage;
