import { Navigate } from "react-router-dom";

import CurtailmentManagementPanel from "@/protoFleet/features/energy/CurtailmentManagementPanel";
import { useHasPermission } from "@/protoFleet/store";

const EnergyPage = () => {
  const canReadCurtailment = useHasPermission("curtailment:read", { scope: "org" });
  const canManageCurtailment = useHasPermission("curtailment:manage", { scope: "org" });

  if (!canReadCurtailment) {
    return <Navigate to="/" replace />;
  }

  return (
    <div className="p-6 laptop:p-10">
      <CurtailmentManagementPanel canManageCurtailment={canManageCurtailment} />
    </div>
  );
};

export default EnergyPage;
