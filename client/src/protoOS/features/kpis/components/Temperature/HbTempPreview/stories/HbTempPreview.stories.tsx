import { type ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";
import HbTempPreviewComponent from "../HbTempPreview";
import { hbData } from "./mocks";
import { criticalTemp } from "@/protoOS/features/kpis/constants";

import { lerp } from "@/shared/utils/math";

export const HbTempPreview = ({ heatRatio }: { heatRatio: number }) => {
  const [heatedHbData, setHeatedHbData] = useState(hbData);

  useEffect(() => {
    const lastTemp = hbData.data[hbData.data.length - 1].value || 0;
    const lastTime = hbData.data[hbData.data.length - 1].datetime || 0;
    setHeatedHbData({
      ...hbData,
      data: [
        {
          value: lerp(lastTemp, criticalTemp + 30, heatRatio),
          datetime: lastTime + 1,
        },
      ],
    });
  }, [heatRatio]);

  return <HbTempPreviewComponent hbData={heatedHbData} />;
};

export default {
  title: "ProtoOS/HbTempPreview",
  args: {
    heatRatio: 0,
  },
  argTypes: {
    heatRatio: {
      control: {
        type: "range",
        min: 0,
        max: 1,
        step: 0.01,
      },
    },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <div className="flex min-h-[100vh] w-full items-center justify-center">
          <Story />
        </div>
      </MemoryRouter>
    ),
  ],
};
