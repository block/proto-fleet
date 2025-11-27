import { useMemo, useState } from "react";
import AlertsModal from "@/protoFleet/features/fleetManagement/components/AlertsModal";
import type { Alert as AlertType } from "@/protoFleet/features/fleetManagement/components/AlertsModal/types";
import { Alert } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

interface AlertsCalloutProps {
  alerts: AlertType[];
  numberOfMinersInFleet: number;
}

const AlertsCallout = ({ alerts, numberOfMinersInFleet }: AlertsCalloutProps) => {
  const [showCallout, setShowCallout] = useState(true);
  const [showModal, setShowModal] = useState(false);

  const numberOfAlerts = useMemo(() => {
    return alerts.length;
  }, [alerts]);

  if (!showCallout || numberOfAlerts === 0) return null;

  return (
    <>
      <div className="mb-10">
        <Callout
          intent={intents.warning}
          prefixIcon={<Alert className="text-text-warning" />}
          title={numberOfAlerts + " miners need attention"}
          subtitle={
            Math.ceil((numberOfAlerts / numberOfMinersInFleet) * 100) +
            "% of your fleet is inactive due to overheating and component failures."
          }
          buttonText="View details"
          buttonOnClick={() => setShowModal(true)}
          dismissible
          onDismiss={() => setShowCallout(false)}
        />
      </div>
      {showModal && <AlertsModal show={showModal} alerts={alerts} onDismiss={() => setShowModal(false)} />}
    </>
  );
};

export default AlertsCallout;
