import AlertStatus from "./AlertStatus";
import { alerts } from "@/protoFleet/features/fleetManagement/components/AlertsModal/stories/mocks";

const AlertStatusWrapper = () => {
  // TODO load alerts from API
  const loading = false;

  return <AlertStatus alerts={alerts} loading={loading} />;
};

export default AlertStatusWrapper;
