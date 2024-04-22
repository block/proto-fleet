import { useCallback, useContext, useEffect, useState } from "react";

import { ApiContext, useCreatePool } from "api";

import { debounce } from "common/utils/utility";

import MiningPools, { PoolInfo } from "components/MiningPools";

const SettingsMiningPools = () => {
  const [pools, setPools] = useState<PoolInfo[]>([]);

  const { poolsInfo } = useContext(ApiContext);
  const { createPool } = useCreatePool();

  useEffect(() => {
    if (poolsInfo.length && !pools.length) {
      const newPools = [...Array(3)].map((_, index) => ({
        url: poolsInfo[index]?.url || "",
        username: poolsInfo[index]?.user || "",
        password: "",
      }));
      setPools(newPools);
    }
  }, [poolsInfo, pools]);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const debouncedSubmitPools = useCallback(
    debounce((newPools: PoolInfo[]) => {
      createPool({
        poolInfo: newPools,
      });
    }),
    [createPool]
  );

  const onChangePools = useCallback(
    (newPools: PoolInfo[]) => {
      setPools(newPools);
      debouncedSubmitPools(newPools);
    },
    [debouncedSubmitPools]
  );

  if (pools.length) {
    return <MiningPools onChange={onChangePools} pools={pools} />;
  }
};

export default SettingsMiningPools;
