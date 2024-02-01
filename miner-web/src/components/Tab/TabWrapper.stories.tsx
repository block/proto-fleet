import { AvgEfficiency, AvgHashrate, Efficiency, Hashrate } from "components/Chart/chart.stories";
import DurationSelector from "components/DurationSelector";

import Tab, { TabWrapper } from ".";

export const Tabs = () => {
  return (
    <TabWrapper>
      <Tab label={"Efficiency"}>
          <Efficiency />
          <DurationSelector className="mt-10" />
        </Tab>
        <Tab label={"Hashrate"}>
          <Hashrate />
          <DurationSelector className="mt-10" />
        </Tab>
        <Tab label={"Compare"}>
          <AvgEfficiency height={200} />
          <div className="mb-6" />
          <AvgHashrate height={200} />
          <DurationSelector className="mt-10" />
        </Tab>
    </TabWrapper>
  );
};

export default {
  component: Tabs,
  title: "Tabs",
};
