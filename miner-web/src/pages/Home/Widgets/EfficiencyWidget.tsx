import { useEffect, useState } from "react";

import InfoWidget from "components/InfoWidget";
import Line from "components/InfoWidget/Line";

const EfficiencyWidget = () => {
  // TODO: get efficiency values from API once implemented
  const [data, setData] = useState([
    { value: 1 },
    { value: 3 },
    { value: 2 },
    { value: 9 },
    { value: 5 },
  ]);

  useEffect(() => {
    let timeoutId = setTimeout(() => {
      const newData = [...data];
      newData.shift();
      newData.push({ value: Math.floor(Math.random() * (10 - 1) + 1) });
      setData(newData);
    }, 5000);

    return () => {
      clearTimeout(timeoutId);
    };
  }, [data]);

  return (
    <InfoWidget
      title="Efficiency"
      value="15.5 J/TH"
      loading={false}
      hasBorder
      stats={<Line data={data} />}
    />
  );
};

export default EfficiencyWidget;
