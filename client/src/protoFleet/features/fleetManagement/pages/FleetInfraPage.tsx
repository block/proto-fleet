import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";

const EMPTY_DEVICES: InfraDeviceItem[] = [];

interface FleetInfraPageProps {
  devices?: InfraDeviceItem[];
  canManage?: boolean;
}

const FleetInfraPage = ({ devices = EMPTY_DEVICES, canManage = true }: FleetInfraPageProps) => (
  <InfraDeviceList devices={devices} canManage={canManage} />
);

export default FleetInfraPage;
