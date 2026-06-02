import CurtailmentManagementPanel from "@/protoFleet/features/energy/CurtailmentManagementPanel";
import { useHasPermission } from "@/protoFleet/store";

const EnergyPage = () => {
  const canManageCurtailment = useHasPermission("curtailment:manage");

  return (
    <div className="p-6 laptop:p-10">
      <CurtailmentManagementPanel canManageCurtailment={canManageCurtailment} />
    </div>
  );
};

export default EnergyPage;
