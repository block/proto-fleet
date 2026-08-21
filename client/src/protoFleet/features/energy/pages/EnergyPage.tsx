import { Navigate } from "react-router-dom";

import CurtailmentManagementPanel from "@/protoFleet/features/energy/CurtailmentManagementPanel";
import { useHasPermission, useRole } from "@/protoFleet/store";

const adminRecoveryRoles = new Set(["ADMIN", "SUPER_ADMIN"]);

const EnergyPage = () => {
  const canReadCurtailment = useHasPermission("curtailment:read");
  const canManageCurtailment = useHasPermission("curtailment:manage");
  const role = useRole();
  const canRecoverCurtailment = canManageCurtailment && adminRecoveryRoles.has(role);

  if (!canReadCurtailment) {
    return <Navigate to="/" replace />;
  }

  return (
    <div className="p-6 laptop:p-10">
      <CurtailmentManagementPanel enableManage={canManageCurtailment} enableRecover={canRecoverCurtailment} />
    </div>
  );
};

export default EnergyPage;
