import { ReactNode } from "react";

import Hashrate from "./Hashrate";
import MinerStatus from "./MinerStatus";
import { type Miner, type RowName } from "./types";

type RowConfig = {
  [K in RowName]?: {
    component?: (miner: Miner, selectedMiners: string[]) => ReactNode;
    width: string;
  };
};

const rowConfig: RowConfig = {
  name: {
    width: "w-24",
  },
  macAddress: {
    width: "w-38",
  },
  status: {
    component: (miner: Miner, selectedMiners: string[]) => {
      return (
        <MinerStatus
          isSelected={selectedMiners.includes(miner.macAddress)}
          status={miner.status}
        />
      );
    },
    width: "w-17",
  },
  hashrate: {
    component: (miner: Miner, selectedMiners: string[]) => {
      void selectedMiners;
      return <Hashrate hashrate={miner.hashrate} />;
    },
    width: "w-41",
  },
  efficiency: {
    component: (miner: Miner) => <>{miner.efficiency} J/TH</>,
    width: "w-38",
  },
  powerUsage: {
    component: (miner: Miner) => <>{miner.powerUsage} kW</>,
    width: "w-38",
  },
  temperature: {
    component: (miner: Miner) => <>{miner.temperature}°c</>,
    width: "w-38",
  },
};

export default rowConfig;
