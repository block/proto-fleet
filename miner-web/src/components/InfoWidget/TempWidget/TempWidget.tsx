import { useMemo, useState } from "react";

import { TemperatureResponseTemperaturedata } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import InfoWidget, { Bar } from "components/InfoWidget";

import { getIntensity } from "../utility";
import TempModal from "./TempModal";

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

  const intensity = useMemo(
    () => getIntensity(temp, 90),
    [temp]
  );

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
