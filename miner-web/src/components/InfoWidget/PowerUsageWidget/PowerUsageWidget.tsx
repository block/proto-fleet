import { useMemo, useState } from "react";

import InfoWidget, { Bar } from "components/InfoWidget";

import { getDisplayValue, getIntensity } from "../utility";
import PowerUsageModal from "./PowerUsageModal";

interface PowerUsageWidgetProps {
  loading?: boolean;
  powerUsage?: string | null;
}

const PowerUsageWidget = ({ loading, powerUsage }: PowerUsageWidgetProps) => {
  const [showModal, setShowModal] = useState(false);
  // TODO: calculate intensity based on the actual data when API returns max value
  const max = 10;

  const intensity = useMemo(() => getIntensity(powerUsage, max), [powerUsage]);

  const displayPowerUsage = useMemo(
    () => powerUsage && `${getDisplayValue(powerUsage)} kW`,
    [powerUsage]
  );

  return (
    <>
      <InfoWidget
        title="Power usage"
        value={displayPowerUsage}
        loading={loading}
        hasBorder
        stats={<Bar intensity={loading ? 0 : intensity} />}
        onClick={loading ? undefined : () => setShowModal(true)}
      />
      {showModal && (
        <PowerUsageModal
          onDismiss={() => setShowModal(false)}
          powerUsage={displayPowerUsage}
        />
      )}
    </>
  );
};

export default PowerUsageWidget;
