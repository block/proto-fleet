import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import { mockInfraDevices } from "@/protoFleet/features/infrastructure/mockInfraDevices";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

interface FleetInfraPageProps {
  devices?: InfraDeviceItem[];
  canManage?: boolean;
}

const FleetInfraPage = ({ devices = mockInfraDevices, canManage }: FleetInfraPageProps) => {
  const canManageInfrastructure = useHasPermission("rack:manage");

  return <InfraDeviceList devices={devices} canManage={canManage ?? canManageInfrastructure} />;
};

export default FleetInfraPage;
