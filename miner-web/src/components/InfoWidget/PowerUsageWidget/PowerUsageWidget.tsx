import { useEffect, useMemo, useState } from "react";

import { Aggregates } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import { Duration } from "components/DurationSelector";
import InfoWidget, { Bar } from "components/InfoWidget";

import { getIntensity } from "../utility";
import PowerUsageModal from "./PowerUsageModal";

interface PowerUsageWidgetProps {
  duration: Duration;
  loading?: boolean;
  powerAggregates?: Aggregates;
  powerValues?: Record<string, number | string>[];
}

const PowerUsageWidget = ({
  duration,
  loading,
  powerAggregates,
  powerValues,
}: PowerUsageWidgetProps) => {
  const [powerUsage, setPowerUsage] = useState<string | number>();
  const [showModal, setShowModal] = useState(false);

  const intensity = useMemo(() => getIntensity(powerUsage, 10), [powerUsage]);

  useEffect(() => {
    setPowerUsage(powerValues?.[powerValues.length - 1]?.value);
  }, [powerValues]);

  return (
    <>
      <InfoWidget
        title="Current power usage"
        value={powerUsage && `${getDisplayValue(powerUsage)} kW`}
        loading={loading}
        hasBorder
        stats={<Bar intensity={loading ? 0 : intensity} />}
        onClick={loading ? undefined : () => setShowModal(true)}
      />
      {showModal && (
        <PowerUsageModal
          onDismiss={() => setShowModal(false)}
          powerUsage={powerUsage}
          powerAggregates={powerAggregates}
          powerValues={powerValues}
          duration={duration}
        />
      )}
    </>
  );
};

export default PowerUsageWidget;
