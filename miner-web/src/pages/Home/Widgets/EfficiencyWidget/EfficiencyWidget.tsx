import { useEffect, useState } from "react";

import InfoWidget from "components/InfoWidget";
import Line from "components/InfoWidget/Line";

import { getDisplayValue } from "../utility";

interface EfficiencyWidgetProps {
  efficiency?: string | number | null;
  efficiencyValues?: Record<string, number>[];
  loading?: boolean;
}

const EfficiencyWidget = ({
  efficiency = "15.5",
  efficiencyValues,
  loading,
}: EfficiencyWidgetProps) => {
  // TODO: get efficiency values from API once implemented
  const [data, setData] = useState(
    efficiencyValues || [
      { value: 1 },
      { value: 3 },
      { value: 2 },
      { value: 9 },
      { value: 5 },
    ]
  );

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

  return (
    <InfoWidget
      title="Efficiency"
      value={efficiency && `${getDisplayValue(efficiency)} J/TH`}
      loading={loading}
      hasBorder
      stats={<Line data={data} />}
    />
  );
};

export default EfficiencyWidget;
