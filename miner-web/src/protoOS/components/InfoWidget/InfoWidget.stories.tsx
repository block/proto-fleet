import { useEffect, useState } from "react";

import InfoWidget, { Bar, Line } from ".";
import { getRandomInt } from "@/shared/utils/utility";

interface InfoWidgetProps {
  hasBorder: boolean;
  loading: boolean;
  intensity: number;
}

export const InfoWidgets = ({
  hasBorder,
  loading,
  intensity,
}: InfoWidgetProps) => {
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
      newData.push({ value: getRandomInt(1, 10) });
      setData(newData);
    }, 5000);

    return () => {
      clearTimeout(timeoutId);
    };
  }, [data]);

  return (
    <div className="flex space-x-6 w-[928px]">
      <InfoWidget
        title="Current efficiency"
        value="15.5 J/TH"
        loading={loading}
        hasBorder={hasBorder}
        stats={<Line data={data} />}
      />
      <InfoWidget
        title="Current power Usage"
        value="3.5 kW"
        loading={loading}
        hasBorder={hasBorder}
        stats={<Bar intensity={intensity} />}
      />
      <InfoWidget
        title="Current Hashrate"
        value="230.2 TH/s"
        loading={loading}
        hasBorder={hasBorder}
      />
    </div>
  );
};

export default {
  title: "Components (protoOS)/Info Widgets",
  args: {
    hasBorder: true,
    loading: false,
    intensity: 3,
  },
  argTypes: {
    hasBorder: { control: "boolean" },
    loading: { control: "boolean" },
    intensity: { control: { type: "range", min: 1, max: 10, step: 1 } },
  },
};
