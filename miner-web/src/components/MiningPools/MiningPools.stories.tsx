import { ElementType, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import MiningPoolsComponent from "./MiningPools";
import { PoolInfo } from "./types";
import { getEmptyPoolsInfo } from "./utility";

interface MiningPoolsProps {
  defaultPoolUrl: string;
  defaultPoolUsername: string;
  backupPool1Url: string;
  backupPool1Username: string;
  backupPool2Url: string;
  backupPool2Username: string;
}

export const MiningPools = ({
  defaultPoolUrl,
  defaultPoolUsername,
  backupPool1Url,
  backupPool1Username,
  backupPool2Url,
  backupPool2Username,
}: MiningPoolsProps) => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  pools[0].url = defaultPoolUrl;
  pools[0].username = defaultPoolUsername;
  pools[1].url = backupPool1Url;
  pools[1].username = backupPool1Username;
  pools[2].url = backupPool2Url;
  pools[2].username = backupPool2Username;

  const onChangePools = (newPools: PoolInfo[]) => {
    setPools(newPools);
  };

  return <MiningPoolsComponent onChange={onChangePools} pools={pools} />;
};

export default {
  title: "Components/Mining Pools",
  args: {
    defaultPoolUrl: "stratum+tcp://stratum.slushpool.com:2222",
    defaultPoolUsername: "proto_mining_sw_test_1",
    backupPool1Url: "stratum+tcp://stratum.slushpool.com:3333",
    backupPool1Username: "proto_mining_sw_test_2",
    backupPool2Url: "stratum+tcp://stratum.slushpool.com:4444",
    backupPool2Username: "proto_mining_sw_test_3",
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
