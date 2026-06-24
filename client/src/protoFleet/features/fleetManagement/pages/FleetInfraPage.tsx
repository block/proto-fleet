import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

interface FleetInfraPageProps {
  devices?: InfraDeviceItem[];
  canManage?: boolean;
}

const FleetInfraPage = ({ devices = [], canManage }: FleetInfraPageProps) => {
  const canManageInfrastructure = useHasPermission("site:manage");

  return <InfraDeviceList devices={devices} canManage={canManage ?? canManageInfrastructure} />;
};

export default FleetInfraPage;
