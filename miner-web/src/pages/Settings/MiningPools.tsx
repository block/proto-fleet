import { useCallback, useContext, useEffect, useState } from "react";

import { ApiContext, useCreatePool } from "api";

import { debounce } from "common/utils/utility";

import MiningPools, { PoolInfo } from "components/MiningPools";
import { ToastType, toastTypes } from "components/Toast";
import StatusToast from "./StatusToast";

const SettingsMiningPools = () => {
  const [pools, setPools] = useState<PoolInfo[]>([]);
  const [toastType, setToastType] = useState<ToastType | null>(null);

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
      setToastType(toastTypes.loading);
      createPool({
        poolInfo: newPools,
        onSuccess: () => {
          setToastType(toastTypes.success);
        },
        onError: () => {
          setToastType(toastTypes.error);
        },
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
    return (
      <>
        <StatusToast onClose={() => setToastType(null)} type={toastType} />
        <MiningPools onChange={onChangePools} pools={pools} />
      </>
    );
  }
};

export default SettingsMiningPools;
