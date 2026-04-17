import { ElementType, useMemo, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import MiningPoolsComponent from "./MiningPools";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import { getEmptyPoolsInfo } from "@/shared/components/MiningPools/utility";

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
  const initialPools = useMemo(() => {
    const poolConfigs = [
      { url: defaultPoolUrl, username: defaultPoolUsername },
      { url: backupPool1Url, username: backupPool1Username },
      { url: backupPool2Url, username: backupPool2Username },
    ];

    return getEmptyPoolsInfo().map((pool, index) => ({
      ...pool,
      ...(poolConfigs[index] || {}),
    }));
  }, [defaultPoolUrl, defaultPoolUsername, backupPool1Url, backupPool1Username, backupPool2Url, backupPool2Username]);

  const [pools, setPools] = useState<PoolInfo[]>(initialPools);

  const onChangePools = (newPools: PoolInfo[]) => {
    setPools(newPools);
  };

  return <MiningPoolsComponent title="Mining pools" onChange={onChangePools} pools={pools} />;
};

export default {
  title: "Shared/Mining Pools",
  parameters: {
    withRouter: false,
  },
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
