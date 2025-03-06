import { useMemo, useState } from "react";

import { getIntensity } from "../utility";
import TempModal from "./TempModal";
import { TemperatureResponseTemperaturedata } from "@/protoOS/api/types";
import InfoWidget, { Bar } from "@/protoOS/components/InfoWidget";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface TempWidgetProps {
  duration?: TemperatureResponseTemperaturedata["duration"];
  hashboardSerials?: string[];
  highestTemp?: number;
  loading?: boolean;
  temp?: number;
}

const TempWidget = ({
  duration,
  hashboardSerials,
  highestTemp,
  loading,
  temp,
}: TempWidgetProps) => {
  const [showModal, setShowModal] = useState(false);

  const intensity = useMemo(() => getIntensity(temp, 90), [temp]);

  return (
    <>
      <InfoWidget
        title="Current miner temperature"
        value={
          temp &&
          // \u00B0c is the degree symbol
          `${getDisplayValue(temp)}\u00B0c`
        }
        loading={loading}
        hasBorder
        stats={<Bar intensity={loading ? 0 : intensity} />}
        onClick={loading ? undefined : () => setShowModal(true)}
      />
      {showModal && hashboardSerials && (
        <TempModal
          temp={temp}
          highestTemp={highestTemp}
          duration={duration}
          hashboardSerials={hashboardSerials}
          onDismiss={() => setShowModal(false)}
        />
      )}
    </>
  );
};

export default TempWidget;
