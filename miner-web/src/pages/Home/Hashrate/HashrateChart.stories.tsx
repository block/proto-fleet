import { Duration } from "components/DurationSelector";

import {
  mockHashrateData,
  mockHashrateData1,
  mockHashrateData2,
  mockHashrateData3,
} from "./constants";
import HashrateChartComponent from "./HashrateChart";
import { convertHashrateValues } from "./utility";

interface HashrateProps {
  duration: Duration;
  hashrates: number;
}

export const HashrateChart = ({ duration, hashrates }: HashrateProps) => {
  return (
    <div className="flex justify-center my-8">
      <div className="w-[928px] h-[400px]">
        <HashrateChartComponent
          duration={duration}
          hashrate1={convertHashrateValues(mockHashrateData1.data)}
          hashrate2={
            hashrates > 1 ? convertHashrateValues(mockHashrateData2.data) : []
          }
          hashrate3={
            hashrates === 3 ? convertHashrateValues(mockHashrateData3.data) : []
          }
          hashrates={convertHashrateValues(mockHashrateData.data)}
        />
      </div>
    </div>
  );
};

export default {
  title: "pages/Home/Hashrate Chart",
  args: {
    duration: "12h",
    hashrates: 3,
  },
  argTypes: {
    duration: {
      control: "select",
      options: ["12h", "24h", "48h", "5d"],
    },
    hashrates: {
      control: { type: "number", min: 1, max: 3 },
    },
  },
};
