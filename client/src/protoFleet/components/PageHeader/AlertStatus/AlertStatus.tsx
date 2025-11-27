import { useMemo, useState } from "react";
import AlertsModal from "@/protoFleet/features/fleetManagement/components/AlertsModal";
import { type Alert as AlertType } from "@/protoFleet/features/fleetManagement/components/AlertsModal/types";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Chip from "@/shared/components/Chip";

interface AlertStatusProps {
  alerts: AlertType[];
  loading?: boolean;
}

const AlertStatus = ({ alerts, loading = false }: AlertStatusProps) => {
  const [showModal, setShowModal] = useState(false);

  const numberOfAlerts = useMemo(() => {
    return alerts.length;
  }, [alerts]);

  const text = () => {
    if (loading) return "Alerts";

    return numberOfAlerts + " Alerts";
  };

  // TODO some alerts probably have higher severity, then icon should be red
  const icon = () => {
    const color = (numberOfAlerts ?? 0) > 0 ? "text-text-warning" : "text-text-success";

    return <Alert className={color} width={iconSizes.small} />;
  };

  if (numberOfAlerts === 0) return null;

  return (
    <>
      <Chip loading={loading} prefixIcon={icon()} onClick={() => setShowModal(true)}>
        {text()}
      </Chip>
      {showModal && <AlertsModal show={showModal} alerts={alerts} onDismiss={() => setShowModal(false)} />}
    </>
  );
};

export default AlertStatus;
