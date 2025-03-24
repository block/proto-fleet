import TabMenu from "./TabMenu";

type TabMenuWrapperProps = {
  hashrate?: number;
  efficiency?: number;
  powerUsage?: number;
  temperature?: number;
};

const TabMenuWrapper = ({
  hashrate,
  efficiency,
  powerUsage,
  temperature,
}: TabMenuWrapperProps) => {
  const tabItems = {
    hashrate: {
      name: "Hashrate",
      value: hashrate,
      units: "TH/S",
      path: "/hashrate",
    },
    efficiency: {
      name: "Efficiency",
      value: efficiency,
      units: "J/TH",
      path: "/efficiency",
    },
    powerUsage: {
      name: "Power Usage",
      value: powerUsage,
      units: "kw/H",
      path: "/power-usage",
    },
    temperature: {
      name: "Temperature",
      value: temperature,
      units: "ºC",
      path: "/temperature",
    },
  };

  return <TabMenu items={tabItems} />;
};

export default TabMenuWrapper;
