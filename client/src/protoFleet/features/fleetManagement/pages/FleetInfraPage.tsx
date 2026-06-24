import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

const EMPTY_DEVICES: InfraDeviceItem[] = [];

interface FleetInfraPageProps {
  devices?: InfraDeviceItem[];
  canManage?: boolean;
}

const FleetInfraPage = ({ devices = EMPTY_DEVICES, canManage }: FleetInfraPageProps) => {
  const canManageInfrastructure = useHasPermission("site:manage");

  return <InfraDeviceList devices={devices} canManage={canManage ?? canManageInfrastructure} />;
};

export default FleetInfraPage;
