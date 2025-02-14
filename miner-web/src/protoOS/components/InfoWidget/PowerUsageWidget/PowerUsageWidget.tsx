import { useEffect, useMemo, useState } from "react";


import { getIntensity } from "../utility";
import PowerUsageModal from "./PowerUsageModal";
import { Aggregates } from "@/protoOS/api/types";
import InfoWidget, { Bar } from "@/protoOS/components/InfoWidget";
import { Duration } from "@/shared/components/DurationSelector";

import { getDisplayValue } from "@/shared/utils/stringUtils";

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
