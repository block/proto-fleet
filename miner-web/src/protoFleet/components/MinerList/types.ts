import { type StatusCircleProps } from "@/shared/components/StatusCircle";

export type Miner = {
  name: string;
  macAddress: string;
  status: {
    hashboard: StatusCircleProps["status"];
    asic: StatusCircleProps["status"];
    fans: StatusCircleProps["status"];
    cb: StatusCircleProps["status"];

    // TODO: these will probably be derived from the above
    hashing: boolean;
    offline: boolean;
    asleep: boolean;
    broken: boolean;
  };
  hashrate: { time: number; hashrate: number }[];
  efficiency: number;
  powerUsage: number;
  temperature: number;
  ip: string;
};

export type RowName = keyof Miner;
