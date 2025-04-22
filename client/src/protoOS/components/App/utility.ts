import { MiningStatusMiningstatus } from "@/protoOS/api/types";

export const isWarmingUp = (miningStatus?: MiningStatusMiningstatus) => {
  const mining = {
    status: miningStatus?.status || "",
    mining_uptime_s: miningStatus?.mining_uptime_s || 0,
    reboot_uptime_s: miningStatus?.reboot_uptime_s || 0,
  };
  // no pools will be returned initially when the miner is warming up or shortly after reboot before connection is established
  return (
    /Uninitialized|PoweringOn|NoPools/i.test(mining.status) &&
    (mining.mining_uptime_s < 60 || mining.reboot_uptime_s < 60)
  );
};

export const isSleeping = (
  miningStatus: MiningStatusMiningstatus["status"],
) => {
  return /PoweringOff|Stopped/i.test(miningStatus || "");
};

export const isMining = (miningStatus: MiningStatusMiningstatus["status"]) => {
  return /Mining/i.test(miningStatus || "");
};

export const isAwake = (miningStatus: MiningStatusMiningstatus["status"]) => {
  return /PoweringOn|Mining|DegradedMining|NoPools|Error/i.test(
    miningStatus || "",
  );
};
