import {
  mockHashrateData,
  mockHashrateData1,
  mockHashrateData2,
  mockHashrateData3,
} from "./constants";
import HashrateChartComponent from "./HashrateChart";
import { aggregateHashrateValues, convertHashrateValues } from "./utility";
import { Duration } from "@/shared/components/DurationSelector";

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
          hashrate1={convertHashrateValues(
            aggregateHashrateValues(mockHashrateData1.data, duration),
          )}
          hashrate2={
            hashrates > 1
              ? convertHashrateValues(
                  aggregateHashrateValues(mockHashrateData2.data, duration),
                )
              : []
          }
          hashrate3={
            hashrates === 3
              ? convertHashrateValues(
                  aggregateHashrateValues(mockHashrateData3.data, duration),
                )
              : []
          }
          hashrates={convertHashrateValues(
            aggregateHashrateValues(mockHashrateData.data, duration),
          )}
        />
      </div>
    </div>
  );
};

export default {
  title: "Components/Hashrate Chart",
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
