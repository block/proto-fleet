import { Navigate } from "react-router-dom";

import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

const EMPTY_DEVICES: InfraDeviceItem[] = [];

interface FleetInfraPageProps {
  devices?: InfraDeviceItem[];
  canRead?: boolean;
  canManage?: boolean;
}

const FleetInfraPage = ({ devices = EMPTY_DEVICES, canRead, canManage }: FleetInfraPageProps) => {
  const canReadSites = useHasPermission("site:read");
  const canManageSites = useHasPermission("site:manage");
  const canReadInfrastructure = canRead ?? canReadSites;
  const canManageInfrastructure = canManage ?? canManageSites;

  if (!canReadInfrastructure) {
    return <Navigate to="/fleet" replace />;
  }

  return <InfraDeviceList devices={devices} canManage={canManageInfrastructure} />;
};

export default FleetInfraPage;
