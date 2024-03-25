import { useEffect, useMemo, useState } from "react";

import InfoWidget from "components/InfoWidget";
import Line from "components/InfoWidget/Line";

import { getDisplayValue } from "../utility";
import EfficiencyModal from "./EfficiencyModal";

interface EfficiencyWidgetProps {
  efficiency?: string | number | null;
  efficiencyValues?: Record<string, number>[];
  loading?: boolean;
}

const EfficiencyWidget = ({
  efficiency,
  efficiencyValues,
  loading,
}: EfficiencyWidgetProps) => {
  const [showModal, setShowModal] = useState(false);
  // TODO: get efficiency values from API once implemented
  const [data, setData] = useState(efficiencyValues || []);

  useEffect(() => {
    if (loading || !efficiencyValues?.length) {
      if (data.length) setData([]);
      return;
    } else if (efficiencyValues.length !== data.length) {
      setData(efficiencyValues);
      return;
    }

    let timeoutId = setTimeout(() => {
      const newData = [...data];
      newData.shift();
      newData.push({ value: Math.floor(Math.random() * (10 - 1) + 1) });
      setData(newData);
    }, 5000);

    return () => {
      clearTimeout(timeoutId);
    };
  }, [data, efficiencyValues, loading]);

  const displayEfficiency = useMemo(
    () => efficiency && `${getDisplayValue(efficiency)} J/TH`,
    [efficiency]
  );

  return (
    <>
      <InfoWidget
        title="Efficiency"
        value={displayEfficiency}
        loading={loading}
        hasBorder
        stats={<Line data={data} />}
        onClick={loading ? undefined : () => setShowModal(true)}
      />
      {showModal && (
        <EfficiencyModal
          onDismiss={() => setShowModal(false)}
          efficiency={displayEfficiency}
        />
      )}
    </>
  );
};

export default EfficiencyWidget;
